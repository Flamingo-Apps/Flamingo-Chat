package googleauth

import "context"

// Fake turns a code of the form "fake:<sub>:<email>" into that same
// string as the "id_token" - identity's own verify.Fake (DEC-0049)
// already knows how to parse exactly that shape, so this lets the whole
// redirect flow (start -> Google -> callback -> identity) be exercised
// end to end in local development and tests without a browser or real
// Google credentials, the same way DEV_FAKE_GOOGLE already does for
// identity on its own.
//
// Never used outside local development: main.go only builds this when
// DEV_FAKE_GOOGLE is set, and logs a warning when it does.
type Fake struct{}

func (Fake) Exchange(_ context.Context, code string) (string, error) {
	return code, nil
}
