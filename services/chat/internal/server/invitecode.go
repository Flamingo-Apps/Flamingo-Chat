package server

import "crypto/rand"

// maxInviteCodeAttempts bounds how many generated codes CreateGroupRoom
// and RotateInviteCode try before giving up.
const maxInviteCodeAttempts = 10

// inviteCodeAlphabet excludes characters that are easy to misread
// (0/O, 1/I/l) when a code is read aloud or typed from memory.
const inviteCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

const inviteCodeLength = 8

// generateInviteCode returns a random 8-character invite code. Unlike
// generatePseudonym, this uses crypto/rand rather than math/rand/v2: an
// invite code is a bearer credential - anyone who has it can join a
// private group room - so unpredictability here is a real security
// property, not just cosmetic variety.
func generateInviteCode() (string, error) {
	b := make([]byte, inviteCodeLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := make([]byte, inviteCodeLength)
	for i, n := range b {
		code[i] = inviteCodeAlphabet[int(n)%len(inviteCodeAlphabet)]
	}
	return string(code), nil
}
