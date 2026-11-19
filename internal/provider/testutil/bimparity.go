package testutil

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

// NormalizeAPIUser reduces an API user to the fields the provider owns.
// Timestamps and server-generated metadata are intentionally ignored.
func NormalizeAPIUser(user *immutaapi.User) map[string]any {
	profile := user.Profile
	if profile == nil {
		profile = &immutaapi.UserProfile{}
	}

	// Always a non-nil slice so that it compares equal to an empty set read
	// out of Terraform state.
	permissions := make([]string, 0, len(user.Permissions))
	permissions = append(permissions, user.Permissions...)
	sort.Strings(permissions)

	return map[string]any{
		"id":           strconv.FormatInt(user.ID, 10),
		"iam_id":       emptyToNil(user.IAMID),
		"user_id":      emptyToNil(user.UserID),
		"name":         emptyToNil(profile.Name),
		"email":        emptyToNil(profile.Email),
		"phone":        emptyToNil(profile.Phone),
		"sql_user":     emptyToNil(profile.SQLUser),
		"about":        emptyToNil(profile.About),
		"location":     emptyToNil(profile.Location),
		"organization": emptyToNil(profile.Organization),
		"position":     emptyToNil(profile.Position),
		"disabled":     user.Disabled,
		"permissions":  permissions,
	}
}

// NormalizeStateUser reduces Terraform state attributes to the same shape as
// NormalizeAPIUser.
func NormalizeStateUser(attributes map[string]string) map[string]any {
	normalized := map[string]any{
		"id":          attributes["id"],
		"disabled":    attributes["disabled"] == "true",
		"permissions": stateSet(attributes, "permissions"),
	}

	for _, key := range []string{"iam_id", "user_id", "name", "email", "phone", "sql_user", "about", "location", "organization", "position"} {
		normalized[key] = emptyToNil(attributes[key])
	}

	return normalized
}

// UserParityError compares Terraform state with the Immuta API and returns an
// error describing any difference.
func UserParityError(user *immutaapi.User, attributes map[string]string) error {
	api := NormalizeAPIUser(user)
	state := NormalizeStateUser(attributes)

	if !reflect.DeepEqual(api, state) {
		return fmt.Errorf("Terraform state and the Immuta API disagree about the user:\n  api:   %s\n  state: %s", format(api), format(state))
	}

	return nil
}

// NormalizeAPIGroup reduces an API group to the fields the provider owns.
func NormalizeAPIGroup(group *immutaapi.Group) map[string]any {
	return map[string]any{
		"id":          strconv.FormatInt(group.ID, 10),
		"iam_id":      emptyToNil(group.IAMID),
		"name":        emptyToNil(group.Name),
		"email":       emptyToNil(group.Email),
		"description": emptyToNil(group.Description),
	}
}

// NormalizeStateGroup reduces Terraform state attributes to the same shape as
// NormalizeAPIGroup.
func NormalizeStateGroup(attributes map[string]string) map[string]any {
	normalized := map[string]any{"id": attributes["id"]}
	for _, key := range []string{"iam_id", "name", "email", "description"} {
		normalized[key] = emptyToNil(attributes[key])
	}

	return normalized
}

// GroupParityError compares Terraform state with the Immuta API and returns
// an error describing any difference.
func GroupParityError(group *immutaapi.Group, attributes map[string]string) error {
	api := NormalizeAPIGroup(group)
	state := NormalizeStateGroup(attributes)

	if !reflect.DeepEqual(api, state) {
		return fmt.Errorf("Terraform state and the Immuta API disagree about the group:\n  api:   %s\n  state: %s", format(api), format(state))
	}

	return nil
}

// stateSet reads a Terraform set attribute out of the flat state map and
// returns its elements in a stable order.
func stateSet(attributes map[string]string, name string) []string {
	count, err := strconv.Atoi(attributes[name+".#"])
	if err != nil || count == 0 {
		return []string{}
	}

	values := make([]string, 0, count)
	for key, value := range attributes {
		if key == name+".#" || len(key) <= len(name)+1 || key[:len(name)+1] != name+"." {
			continue
		}
		values = append(values, value)
	}
	sort.Strings(values)

	return values
}
