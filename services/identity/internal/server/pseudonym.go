package server

import (
	"fmt"
	"math/rand/v2"
)

// pseudonymPlainAttempts is how many tries get a plain two-word name before
// a number is added. Names must be unique, and there are only 900 two-word
// combinations: once a few hundred accounts exist, plain names clash often,
// and with the web app choosing from an even smaller list guests failed with
// "pseudonym already taken" (INC-0025). Adding two digits after a few clashes
// makes the space about 80,000 while most people still get a plain name.
const pseudonymPlainAttempts = 3

// generatePseudonym returns a random two-word pseudonym like "Silent Otter".
func generatePseudonym() string {
	//nolint:gosec // a display name, not a token or session id - predictability here isn't a security concern
	return pseudonymAdjectives[rand.IntN(len(pseudonymAdjectives))] + " " + pseudonymNouns[rand.IntN(len(pseudonymNouns))]
}

// generatePseudonymAttempt is the name to try on the given attempt (counting
// from 0): two words at first, then two words and a two-digit number, like
// "Silent Otter 42". The caller retries on a collision.
func generatePseudonymAttempt(attempt int) string {
	name := generatePseudonym()
	if attempt < pseudonymPlainAttempts {
		return name
	}
	//nolint:gosec // a display name, not a secret
	return fmt.Sprintf("%s %d", name, 10+rand.IntN(90))
}

// nextPseudonym is generatePseudonymAttempt, as a variable only so tests can
// force collisions.
var nextPseudonym = generatePseudonymAttempt

var pseudonymAdjectives = []string{
	"Silent", "Quiet", "Brave", "Calm", "Curious", "Gentle", "Happy", "Lucky",
	"Bold", "Clever", "Swift", "Bright", "Quick", "Wild", "Kind", "Witty",
	"Sly", "Proud", "Mellow", "Vivid", "Eager", "Jolly", "Nimble", "Rapid",
	"Daring", "Fuzzy", "Golden", "Hidden", "Misty", "Noble",
}

var pseudonymNouns = []string{
	"Otter", "Falcon", "Fox", "Panda", "Tiger", "Wolf", "Hawk", "Heron",
	"Lynx", "Raven", "Badger", "Dolphin", "Eagle", "Gecko", "Ibis", "Jay",
	"Koala", "Lemur", "Moose", "Newt", "Owl", "Puffin", "Quokka", "Robin",
	"Seal", "Toucan", "Urchin", "Viper", "Walrus", "Yak",
}
