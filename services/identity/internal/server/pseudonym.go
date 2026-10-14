package server

import "math/rand/v2"

// generatePseudonym returns a random two-word pseudonym like "Silent
// Otter", for an account SignInWithGoogle creates directly with no guest
// account to carry an existing pseudonym over from (docs/ARCHITECTURE.md
// §4.2). The caller is responsible for retrying with a new one on a
// collision - 900 combinations is plenty given how few sign-ins are
// expected to take this path (most start as a guest first).
func generatePseudonym() string {
	//nolint:gosec // a display name, not a token or session id - predictability here isn't a security concern
	return pseudonymAdjectives[rand.IntN(len(pseudonymAdjectives))] + " " + pseudonymNouns[rand.IntN(len(pseudonymNouns))]
}

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
