package validationrules

import (
	"testing"

	govalidate "github.com/xloss/go-validate"
	"github.com/xloss/go-validate/rules"
)

type isinValidationInput struct {
	ISIN string `json:"isin"`
}

func TestISINRuleAcceptsKnownValidIdentifiers(t *testing.T) {
	t.Parallel()

	valid := []string{
		"RU000A101NK4",
		"KZ1C00001122",
		"US3563901046",
		"US00449L1026",
	}

	for _, isin := range valid {
		_, validationErrors := govalidate.Run[isinValidationInput](
			map[string]any{"isin": isin},
			map[string][]any{"isin": {rules.Required{}, rules.String{}, ISIN{}}},
		)
		if len(validationErrors) != 0 {
			t.Fatalf("ISIN %q validation errors = %#v", isin, validationErrors)
		}
	}
}

func TestISINRuleRejectsInvalidIdentifiers(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"RU000A101NK5",
		"KZ1C0000112",
		"KZ1C0000112!",
		"ru000a101nk4",
		"",
	}

	for _, isin := range invalid {
		_, validationErrors := govalidate.Run[isinValidationInput](
			map[string]any{"isin": isin},
			map[string][]any{"isin": {rules.Required{}, rules.String{}, ISIN{}}},
		)
		if len(validationErrors) == 0 {
			t.Fatalf("ISIN %q unexpectedly passed validation", isin)
		}
	}
}

func TestISINRuleIsOptionalWithoutRequired(t *testing.T) {
	t.Parallel()

	_, validationErrors := govalidate.Run[isinValidationInput](
		map[string]any{},
		map[string][]any{"isin": {ISIN{}}},
	)
	if len(validationErrors) != 0 {
		t.Fatalf("missing optional ISIN validation errors = %#v", validationErrors)
	}
}
