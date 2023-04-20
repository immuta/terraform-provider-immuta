package immuta

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const testBimGroupId = "tf_test_group_acc"
const testBimGroupIamId = "bim"
const testBimGroupName = "Test Group TF ACC"
const testBimGroupEmail = "tf_acc_test_group@instacart.com"
const testBimGroupDescription = "test description"
const testBimGroupUpdatedDescription = "test updated description"

func TestAccBimGroup_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckBimGroupDestroy,
		Steps: []resource.TestStep{
			// test create and read
			{
				Config: testAccBimGroupConfigBasic(testBimGroupDescription),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_group.test", "iamid", testBimGroupIamId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group.test", "name", testBimGroupName),
					resource.TestCheckResourceAttr(
						"immuta_bim_group.test", "email", testBimGroupEmail),
					resource.TestCheckResourceAttr(
						"immuta_bim_group.test", "description", testBimGroupDescription),
				),
			},
			//test update and read
			{
				Config: testAccBimGroupConfigBasic(testBimGroupUpdatedDescription),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_group.test", "iamid", testBimGroupIamId),
					resource.TestCheckResourceAttr(
						"immuta_bim_group.test", "name", testBimGroupName),
					resource.TestCheckResourceAttr(
						"immuta_bim_group.test", "email", testBimGroupEmail),
					resource.TestCheckResourceAttr(
						"immuta_bim_group.test", "description", testBimGroupUpdatedDescription),
				),
			},
		},
	})

}

func testAccCheckBimGroupDestroy(s *terraform.State) error {
	return nil
}

func testAccBimGroupConfigBasic(description string) string {
	return `
	resource "immuta_bim_group" "test" {
		  iamid        = "` + testBimGroupIamId + `"
		  name        = "` + testBimGroupName + `"
		  email = "` + testBimGroupEmail + `"
		  description = "` + description + `"	
	}
`
}
