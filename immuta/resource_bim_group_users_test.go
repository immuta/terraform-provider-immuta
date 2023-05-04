package immuta

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const testGroupId = "32"

const testUser1UserId = "tf_acc_test_user1@instacart.com"
const testUser1ProfileId = "119"

const testUser2UserId = "tf_acc_test_user2@instacart.com"
const testUser2ProfileId = "120"

var usersMap = map[string]string{
	"user1": `{
				group = "` + testGroupId + `"
				userid = "` + testUser1UserId + `"
				iamid = "immuta"
				profile = ` + testUser1ProfileId + `
			},`,
	"user2": `{
				group = "` + testGroupId + `"
				userid = "` + testUser2UserId + `"
				iamid = "immuta"
				profile = ` + testUser2ProfileId + `
			},`,
}

func TestAccBimGroupUsers_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckBimGroupDestroy,
		Steps: []resource.TestStep{
			// test create and read
			{
				Config: testAccBimGroupUsersConfigBasic([]string{"user1"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "id", testGroupId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.0.userid", testUser1UserId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.0.profile", testUser1ProfileId),
				),
			},
			//test update (add a new user) and read
			{
				Config: testAccBimGroupUsersConfigBasic([]string{"user1", "user2"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "id", testGroupId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.0.userid", testUser1UserId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.0.profile", testUser1ProfileId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.1.userid", testUser2UserId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.1.profile", testUser2ProfileId),
				),
			},
			// test update (remove a user) and read
			{
				Config: testAccBimGroupUsersConfigBasic([]string{"user2"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "id", testGroupId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.0.userid", testUser2UserId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.0.profile", testUser2ProfileId),
				),
			},
		},
	})

}

func testAccCheckBimGroupUsersDestroy(s *terraform.State) error {
	return nil
}

func testAccBimGroupUsersConfigBasic(users []string) string {
	var usersList = ""
	for _, userName := range users {
		usersList = usersList + usersMap[userName]
	}
	return `
	resource "immuta_bim_group_users" "test" {
			users = [
				` + usersList + `
			]
		}
	`
}
