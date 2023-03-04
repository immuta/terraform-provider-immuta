package immuta

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"testing"
)

const testBimUserId = "tf_test_user_acc"
const testBimUserName = "Test User"
const testBimUserEmail = "tf_test_user@test.com"
const testBimUserPassword = "test_password"

func TestAccBimUser_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckBimUserDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBimUserConfigBasic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "userid", testBimUserId),
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "password", testBimUserPassword),
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "email", testBimUserEmail),
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "snowflake_user", testBimUserId),
				),
			},
		},
	})
}

func TestAccBimUser_WithName(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckBimUserDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBimUserConfigWithName(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "userid", testBimUserId),
					resource.TestCheckResourceAttr(
						"immuta_bim_user.test", "name", testBimUserName),
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

func testAccBimUserConfigBasic() string {
	return `
	resource "immuta_bim_user" "test" {
		  userid        = "` + testBimUserId + `"
		  password = "` + testBimUserPassword + `"
		  email = "` + testBimUserEmail + `"
		  snowflake_user = "` + testBimUserId + `"	
	}
`
}

func testAccBimUserConfigWithName() string {
	return `
	resource "immuta_bim_user" "test" {
		  userid        = "` + testBimUserId + `"
          name         = "` + testBimUserName + `"
		  password = "` + testBimUserPassword + `"
		  email = "` + testBimUserEmail + `"
		  snowflake_user = "` + testBimUserId + `"	
	}
`
}
