package ws

import (
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

func toAccountJSON(acc *identityv1.Account) accountJSON {
	return accountJSON{
		ID:            acc.GetId(),
		Pseudonym:     acc.GetPseudonym(),
		Gender:        genderString(acc.GetGender()),
		GenderChosen:  acc.GetGenderChosen(),
		GoogleLinked:  acc.GetGoogleLinked(),
		CreatedAtUnix: acc.GetCreatedAtUnix(),
	}
}

// genderString mirrors internal/httpapi's own copy - two small packages,
// each self-contained, rather than a shared package for one four-line
// switch.
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
