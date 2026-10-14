package httpapi

import (
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

// accountResponse is the JSON shape returned for an account anywhere the
// HTTP API includes one. Field names are the proto field names in
// snake_case, the same convention docs/ARCHITECTURE.md §5.2 uses for the
// WebSocket protocol's JSON frames.
type accountResponse struct {
	ID            string `json:"id"`
	Pseudonym     string `json:"pseudonym"`
	Gender        string `json:"gender"`
	GenderChosen  bool   `json:"gender_chosen"`
	GoogleLinked  bool   `json:"google_linked"`
	CreatedAtUnix int64  `json:"created_at_unix"`
}

func toAccountResponse(acc *identityv1.Account) accountResponse {
	return accountResponse{
		ID:            acc.GetId(),
		Pseudonym:     acc.GetPseudonym(),
		Gender:        genderString(acc.GetGender()),
		GenderChosen:  acc.GetGenderChosen(),
		GoogleLinked:  acc.GetGoogleLinked(),
		CreatedAtUnix: acc.GetCreatedAtUnix(),
	}
}

// genderString renders identity.v1.Gender the way an HTTP API caller
// sends and expects it - "male", not the proto enum's own
// "GENDER_MALE" spelling.
func genderString(g identityv1.Gender) string {
	switch g {
	case identityv1.Gender_GENDER_MALE:
		return "male"
	case identityv1.Gender_GENDER_FEMALE:
		return "female"
	case identityv1.Gender_GENDER_PREFER_NOT_TO_SAY:
		return "prefer_not_to_say"
	default:
		return "unspecified"
	}
}

// genderFromString is genderString's inverse, for request bodies.
func genderFromString(s string) identityv1.Gender {
	switch s {
	case "male":
		return identityv1.Gender_GENDER_MALE
	case "female":
		return identityv1.Gender_GENDER_FEMALE
	case "prefer_not_to_say":
		return identityv1.Gender_GENDER_PREFER_NOT_TO_SAY
	default:
		return identityv1.Gender_GENDER_UNSPECIFIED
	}
}
