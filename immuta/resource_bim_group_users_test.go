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

func TestAccBimGroupUsers_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckBimGroupDestroy,
		Steps: []resource.TestStep{
			// test create and read
			{
				Config: testAccBimGroupUsersConfigBasic(testUser1UserId, testUser1ProfileId),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "id", testGroupId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.0.userid", testUser1UserId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group_users.test", "users.0.profile", testUser1ProfileId),
				),
			},
			//test update and read
			{
				Config: testAccBimGroupUsersConfig_Update(testUser1UserId, testUser1ProfileId, testUser2UserId, testUser2ProfileId),
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
		},
	})

}

func testAccCheckBimGroupUsersDestroy(s *terraform.State) error {
	return nil
}

func testAccBimGroupUsersConfigBasic(userId1 string, userProfile1 string) string {
	return `
	resource "immuta_bim_group_users" "test" {
			users = [
				{
					group = "` + testGroupId + `"
					userid = "` + userId1 + `"
					iamid = "immuta"
					profile = ` + userProfile1 + `
				},
			]
		}
	`
}

func testAccBimGroupUsersConfig_Update(userId1 string, userProfile1 string, userId2 string, userProfile2 string) string {
	return `
	resource "immuta_bim_group_users" "test" {
			users = [
				{
					group = "` + testGroupId + `"
					userid = "` + userId1 + `"
					iamid = "immuta"
					profile = ` + userProfile1 + `
				},
				{
					group = "` + testGroupId + `"
					userid = "` + userId2 + `"
					iamid = "immuta"
					profile = ` + userProfile2 + `
				}
			]
		}
	`
}
