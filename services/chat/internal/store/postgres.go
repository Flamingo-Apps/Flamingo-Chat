package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
)

// Postgres error codes - see
// https://www.postgresql.org/docs/current/errcodes-appendix.html.
const (
	postgresUniqueViolation           = "23505"
	postgresInvalidTextRepresentation = "22P02" // e.g. a non-UUID string where a uuid column is expected
)

// roomColumns is the column list every room query below selects, in the
// order scanRoom expects.
const roomColumns = "id, type, name, owner_account_id, invite_code, kept, created_at, ended_at"

// messageColumns is the column list every message query below selects, in
// the order scanMessage expects.
const messageColumns = "message_id, room_id, sender_account_id, sender_pseudonym, body, sent_at"

// Postgres is the RoomStore implementation backed by the rooms,
// room_members and keep_votes tables (and reads from messages, which only
// the persistence worker writes to - see store.go's package comment).
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) CreateGroupRoom(ctx context.Context, id, ownerAccountID, name, inviteCode string) (Room, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Room{}, fmt.Errorf("store: create group room: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := `
		INSERT INTO rooms (id, type, name, owner_account_id, invite_code)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + roomColumns
	room, err := scanRoom(tx.QueryRow(ctx, q, id, chatv1.RoomType_ROOM_TYPE_GROUP, name, ownerAccountID, inviteCode))
	if isUniqueViolation(err) {
		return Room{}, ErrAlreadyExists
	}
	if err != nil {
		return Room{}, fmt.Errorf("store: create group room: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO room_members (room_id, account_id) VALUES ($1, $2)`, id, ownerAccountID); err != nil {
		return Room{}, fmt.Errorf("store: create group room: add owner: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Room{}, fmt.Errorf("store: create group room: commit: %w", err)
	}
	return room, nil
}

func (p *Postgres) CreateDirectRoom(ctx context.Context, id string, memberAccountIDs []string) (Room, []ReplacedRoom, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Room{}, nil, fmt.Errorf("store: create direct room: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := `INSERT INTO rooms (id, type) VALUES ($1, $2) RETURNING ` + roomColumns
	room, err := scanRoom(tx.QueryRow(ctx, q, id, chatv1.RoomType_ROOM_TYPE_DIRECT))
	if err != nil {
		return Room{}, nil, fmt.Errorf("store: create direct room: %w", err)
	}

	for _, accountID := range memberAccountIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO room_members (room_id, account_id) VALUES ($1, $2)`, id, accountID); err != nil {
			return Room{}, nil, fmt.Errorf("store: create direct room: add member: %w", err)
		}
	}

	// One random chat at a time: end any other open, unsaved direct room that
	// either member is still in. A tab closed mid-chat never ended its room,
	// so without this a person could be in two at once and nothing would say
	// which one Next or End meant (DEC-0112). The member is picked per room so
	// the caller can say who left.
	rows, err := tx.Query(ctx, `
		UPDATE rooms r SET ended_at = now()
		WHERE r.type = $2 AND r.ended_at IS NULL AND NOT r.kept AND r.id <> $1
		  AND EXISTS (SELECT 1 FROM room_members m WHERE m.room_id = r.id AND m.account_id = ANY($3::uuid[]) AND m.left_at IS NULL)
		RETURNING r.id,
		  (SELECT m.account_id FROM room_members m
		    WHERE m.room_id = r.id AND m.account_id = ANY($3::uuid[]) AND m.left_at IS NULL
		    ORDER BY m.account_id LIMIT 1)`,
		id, chatv1.RoomType_ROOM_TYPE_DIRECT, memberAccountIDs)
	if err != nil {
		return Room{}, nil, fmt.Errorf("store: create direct room: end earlier rooms: %w", err)
	}
	var replaced []ReplacedRoom
	for rows.Next() {
		var r ReplacedRoom
		if err := rows.Scan(&r.RoomID, &r.MemberAccountID); err != nil {
			rows.Close()
			return Room{}, nil, fmt.Errorf("store: create direct room: scan earlier room: %w", err)
		}
		replaced = append(replaced, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Room{}, nil, fmt.Errorf("store: create direct room: end earlier rooms: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Room{}, nil, fmt.Errorf("store: create direct room: commit: %w", err)
	}
	return room, replaced, nil
}

func (p *Postgres) GetRoom(ctx context.Context, id string) (Room, error) {
	q := `SELECT ` + roomColumns + ` FROM rooms WHERE id = $1`
	room, err := scanRoom(p.pool.QueryRow(ctx, q, id))
	if isNotFound(err) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("store: get room: %w", err)
	}
	return room, nil
}

func (p *Postgres) GetRoomByInviteCode(ctx context.Context, inviteCode string) (Room, error) {
	q := `SELECT ` + roomColumns + ` FROM rooms WHERE invite_code = $1`
	room, err := scanRoom(p.pool.QueryRow(ctx, q, inviteCode))
	if isNotFound(err) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("store: get room by invite code: %w", err)
	}
	return room, nil
}

func (p *Postgres) RotateInviteCode(ctx context.Context, roomID, newInviteCode string) (Room, error) {
	q := `UPDATE rooms SET invite_code = $2 WHERE id = $1 RETURNING ` + roomColumns
	room, err := scanRoom(p.pool.QueryRow(ctx, q, roomID, newInviteCode))
	if isUniqueViolation(err) {
		return Room{}, ErrAlreadyExists
	}
	if isNotFound(err) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("store: rotate invite code: %w", err)
	}
	return room, nil
}

func (p *Postgres) AddMember(ctx context.Context, roomID, accountID string) error {
	q := `
		INSERT INTO room_members (room_id, account_id)
		VALUES ($1, $2)
		ON CONFLICT (room_id, account_id) DO UPDATE SET left_at = NULL
	`
	if _, err := p.pool.Exec(ctx, q, roomID, accountID); err != nil {
		return fmt.Errorf("store: add member: %w", err)
	}
	return nil
}

func (p *Postgres) RemoveMember(ctx context.Context, roomID, accountID string) error {
	q := `UPDATE room_members SET left_at = now() WHERE room_id = $1 AND account_id = $2 AND left_at IS NULL`
	if _, err := p.pool.Exec(ctx, q, roomID, accountID); err != nil {
		return fmt.Errorf("store: remove member: %w", err)
	}
	return nil
}

func (p *Postgres) IsCurrentMember(ctx context.Context, roomID, accountID string) (bool, error) {
	q := `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = $1 AND account_id = $2 AND left_at IS NULL)`
	var ok bool
	if err := p.pool.QueryRow(ctx, q, roomID, accountID).Scan(&ok); err != nil {
		return false, fmt.Errorf("store: is current member: %w", err)
	}
	return ok, nil
}

func (p *Postgres) HasEverBeenMember(ctx context.Context, roomID, accountID string) (bool, error) {
	q := `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = $1 AND account_id = $2)`
	var ok bool
	if err := p.pool.QueryRow(ctx, q, roomID, accountID).Scan(&ok); err != nil {
		return false, fmt.Errorf("store: has ever been member: %w", err)
	}
	return ok, nil
}

func (p *Postgres) MembershipState(ctx context.Context, roomID, accountID string) (bool, bool, error) {
	q := `
		SELECT r.ended_at IS NULL
		FROM room_members m
		JOIN rooms r ON r.id = m.room_id
		WHERE m.room_id = $1 AND m.account_id = $2 AND m.left_at IS NULL
	`
	var open bool
	err := p.pool.QueryRow(ctx, q, roomID, accountID).Scan(&open)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("store: membership state: %w", err)
	}
	return true, open, nil
}

func (p *Postgres) ListRoomsForAccount(ctx context.Context, accountID string) ([]Room, error) {
	q := `
		SELECT ` + prefixColumns("r", roomColumns) + `
		FROM rooms r
		JOIN room_members m ON m.room_id = r.id
		WHERE m.account_id = $1
		  AND m.left_at IS NULL
		  AND (
		        r.type = $2
		     OR (r.kept AND EXISTS (SELECT 1 FROM keep_votes v WHERE v.room_id = r.id AND v.account_id = $1))
		     OR (NOT r.kept AND r.ended_at IS NULL AND r.created_at > now() - make_interval(secs => $3))
		  )
		ORDER BY r.created_at DESC
	`
	rows, err := p.pool.Query(ctx, q, accountID, chatv1.RoomType_ROOM_TYPE_GROUP, ActiveDirectRoomWindow.Seconds())
	if err != nil {
		return nil, fmt.Errorf("store: list rooms for account: %w", err)
	}
	defer rows.Close()

	var rooms []Room
	var ids []string
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list rooms for account: scan: %w", err)
		}
		rooms = append(rooms, room)
		ids = append(ids, room.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list rooms for account: %w", err)
	}
	if len(rooms) == 0 {
		return rooms, nil
	}

	// One more query for everyone's membership, rather than one per room. It
	// includes people who have since left: a chat the other person deleted
	// must still be listed under their name.
	mrows, err := p.pool.Query(ctx, `SELECT room_id, account_id FROM room_members WHERE room_id = ANY($1) ORDER BY account_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("store: list rooms for account: members: %w", err)
	}
	defer mrows.Close()
	byRoom := map[string][]string{}
	for mrows.Next() {
		var roomID, memberID string
		if err := mrows.Scan(&roomID, &memberID); err != nil {
			return nil, fmt.Errorf("store: list rooms for account: scan member: %w", err)
		}
		byRoom[roomID] = append(byRoom[roomID], memberID)
	}
	if err := mrows.Err(); err != nil {
		return nil, fmt.Errorf("store: list rooms for account: members: %w", err)
	}
	for i := range rooms {
		rooms[i].MemberAccountIDs = byRoom[rooms[i].ID]
	}
	return rooms, nil
}

func (p *Postgres) VoteKeep(ctx context.Context, roomID, accountID string) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("store: vote keep: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Locks the room row for the rest of this transaction, so two
	// concurrent votes for the same room can't both read a stale vote
	// count and neither one observes "kept". endedAt also drives the
	// grace-window solo-keep check below.
	var endedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT ended_at FROM rooms WHERE id = $1 FOR UPDATE`, roomID).Scan(&endedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("store: vote keep: lock room: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO keep_votes (room_id, account_id) VALUES ($1, $2)
		ON CONFLICT (room_id, account_id) DO NOTHING
	`, roomID, accountID); err != nil {
		return false, fmt.Errorf("store: vote keep: insert vote: %w", err)
	}

	var votes, members int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM keep_votes WHERE room_id = $1`, roomID).Scan(&votes); err != nil {
		return false, fmt.Errorf("store: vote keep: count votes: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM room_members WHERE room_id = $1 AND left_at IS NULL`, roomID).Scan(&members); err != nil {
		return false, fmt.Errorf("store: vote keep: count members: %w", err)
	}

	kept := members > 0 && votes >= members
	// Grace-window solo keep (DEC-0091): the room already ended, so the
	// member who left can never vote - requiring votes >= members would
	// make "keep" permanently impossible once one side is gone. Within
	// VoteKeepGraceWindow of ending, the remaining member's own vote is
	// enough on its own.
	if !kept && endedAt != nil && time.Since(*endedAt) <= VoteKeepGraceWindow {
		kept = votes >= 1
	}
	if kept {
		if _, err := tx.Exec(ctx, `UPDATE rooms SET kept = true WHERE id = $1`, roomID); err != nil {
			return false, fmt.Errorf("store: vote keep: set kept: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("store: vote keep: commit: %w", err)
	}
	return kept, nil
}

func (p *Postgres) CloseDirectRoom(ctx context.Context, roomID string) error {
	q := `UPDATE rooms SET ended_at = now() WHERE id = $1 AND ended_at IS NULL`
	if _, err := p.pool.Exec(ctx, q, roomID); err != nil {
		return fmt.Errorf("store: close direct room: %w", err)
	}
	return nil
}

func (p *Postgres) EndDirectRoom(ctx context.Context, roomID string) error {
	// Idempotent and deliberately silent about whether anything actually
	// changed - the same "best-effort, caller already knows what it
	// needs to know" philosophy as identity's Touch.
	q := `UPDATE rooms SET ended_at = now() WHERE id = $1 AND ended_at IS NULL AND NOT kept`
	if _, err := p.pool.Exec(ctx, q, roomID); err != nil {
		return fmt.Errorf("store: end direct room: %w", err)
	}
	return nil
}

func (p *Postgres) ListMessages(ctx context.Context, roomID, beforeMessageID string, limit int) ([]Message, error) {
	var rows pgx.Rows
	var err error
	if beforeMessageID == "" {
		rows, err = p.pool.Query(ctx, `
			SELECT `+messageColumns+`
			FROM messages
			WHERE room_id = $1
			ORDER BY sent_at DESC, message_id DESC
			LIMIT $2
		`, roomID, limit)
	} else {
		rows, err = p.pool.Query(ctx, `
			SELECT `+messageColumns+`
			FROM messages
			WHERE room_id = $1
			  AND (sent_at, message_id) < (SELECT sent_at, message_id FROM messages WHERE message_id = $2)
			ORDER BY sent_at DESC, message_id DESC
			LIMIT $3
		`, roomID, beforeMessageID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list messages: scan: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	return messages, nil
}

// prefixColumns qualifies each column in a comma-separated list with a
// table alias, so roomColumns can be shared between a plain "SELECT ...
// FROM rooms" and a join that needs "r.id, r.type, ...".
func prefixColumns(alias, columns string) string {
	parts := strings.Split(columns, ", ")
	for i, c := range parts {
		parts[i] = alias + "." + c
	}
	return strings.Join(parts, ", ")
}

func scanRoom(row pgx.Row) (Room, error) {
	var (
		room     Room
		roomType int32
	)
	if err := row.Scan(&room.ID, &roomType, &room.Name, &room.OwnerAccountID, &room.InviteCode, &room.Kept, &room.CreatedAt, &room.EndedAt); err != nil {
		return Room{}, err
	}
	room.Type = chatv1.RoomType(roomType)
	return room, nil
}

func scanMessage(row pgx.Row) (Message, error) {
	var msg Message
	if err := row.Scan(&msg.MessageID, &msg.RoomID, &msg.SenderAccountID, &msg.SenderPseudonym, &msg.Body, &msg.SentAt); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation
}

// isNotFound covers both "the query found no row" (pgx.ErrNoRows) and "the
// id wasn't even a well-formed UUID" (Postgres rejects it before the query
// can run at all) - the same reasoning as identity's isNotFound
// (INC-0003).
func isNotFound(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresInvalidTextRepresentation
}
