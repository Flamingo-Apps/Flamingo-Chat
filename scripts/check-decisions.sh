#!/usr/bin/env bash
# Validates docs/decisions.jsonl: every line is valid JSON, decision and
# incident entries carry their required fields, ids are unique, and every
# supersedes/superseded_by reference points at an id that actually exists.
# Run locally with no arguments, or pass a different file to check.
set -euo pipefail

FILE="${1:-docs/decisions.jsonl}"

if [ ! -f "$FILE" ]; then
	echo "no such file: $FILE" >&2
	exit 1
fi

# Fails fast with jq's own parse error and line number if any line isn't
# valid JSON.
jq -e . "$FILE" >/dev/null

report=$(jq -s '
  def required_fields:
    if .type == "decision" then
      ["id","type","date","status","area","title","context","options","decision","rationale","tradeoffs","refs","logged_at"]
    elif .type == "incident" then
      ["id","type","date","status","severity","area","title","what_happened","impact","root_cause","resolution","lessons","refs","logged_at"]
    else
      ["type"]
    end;

  . as $all
  | ($all | map(.id)) as $ids
  | [
      # unknown "type" value
      ( $all[] | select(.type != "decision" and .type != "incident")
        | "\(.id // "?"): unknown type \(.type // "(missing)")" ),

      # missing required fields
      ( $all[] | . as $e | (required_fields - (keys)) as $missing
        | select(($missing | length) > 0)
        | "\($e.id): missing field(s) \($missing | join(", "))" ),

      # date looks like YYYY-MM-DD
      ( $all[] | select(.date != null and (.date | test("^\\d{4}-\\d{2}-\\d{2}$") | not))
        | "\(.id): date \(.date) is not YYYY-MM-DD" ),

      # duplicate ids
      ( $ids | group_by(.) | map(select(length > 1) | .[0]) | .[]
        | "duplicate id: \(.)" ),

      # supersedes / superseded_by must reference a real id
      ( $all[] | . as $e
        | ([$e.supersedes, $e.superseded_by] | map(select(. != null)))[]
        | select(. as $ref | ($ids | index($ref)) | not)
        | "\($e.id): references unknown id \(.)" ),

      # a superseded decision must name what superseded it, and vice versa
      ( $all[] | select(.status == "superseded" and (.superseded_by == null))
        | "\(.id): status is superseded but superseded_by is not set" )
    ]
  | map(select(. != null))
' "$FILE")

problems=$(echo "$report" | jq -r 'length')
if [ "$problems" != "0" ]; then
	echo "docs/decisions.jsonl has problems:" >&2
	echo "$report" | jq -r '.[]' >&2
	exit 1
fi

count=$(wc -l <"$FILE" | tr -d ' ')
echo "$FILE: $count entries, all valid"
