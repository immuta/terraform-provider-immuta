package immuta

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"testing"
)

const testBimUserId = "tf_test_user_acc"
const testBimUserEmail = "tf_test_user@test.com"
const testBimUserPassword = "test_password"

func TestAccBimUser_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories(&testAccProviders),
		CheckDestroy:      testAccCheckBimUserDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBimUserConfig(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "userid", testBimUserId),
					// todo how to check for sensitive values?
					//resource.TestCheckResourceAttr(
					//	"immuta_bim_user.test", "password", testBimUserPassword),
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "email", testBimUserEmail),
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "snowflake_user", testBimUserId),
				),
			},
		},
	})
}

func testAccCheckBimUserDestroy(s *terraform.State) error {
	return nil
}

func testAccBimUserConfig() string {
	return `
	resource "immuta_bim_user" "test" {
		  userid        = "` + testBimUserId + `"
		  password = "` + testBimUserPassword + `"
		  email = "` + testBimUserEmail + `"
		  snowflake_user = "` + testBimUserId + `"	
	}
`
}
