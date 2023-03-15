package immuta

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"testing"
)

const testAccBimAttributeUser = "tf_acc_test_user_attributes"

func TestAccBimAttribute_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckBimAttributeDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBimAttributeConfigBasic("tf_acc_test_key", "tf_acc_test_value"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_bim_attribute.test", "iam_id", "bim"),
					resource.TestCheckResourceAttr(
						"immuta_bim_attribute.test", "model_type", "user"),
					resource.TestCheckResourceAttr(
						"immuta_bim_attribute.test", "model_id", testAccBimAttributeUser),
					resource.TestCheckResourceAttr(
						"immuta_bim_attribute.test", "key", "tf_acc_test_key"),
					resource.TestCheckResourceAttr(
						"immuta_bim_attribute.test", "value", "tf_acc_test_value"),
				),
			},
		},
	})

}

func testAccCheckBimAttributeDestroy(_ *terraform.State) error {
	return nil
}

func testAccBimAttributeConfigBasic(key string, value string) string {
	return `
	resource "immuta_bim_user" "test" {
		  userid        = "` + testAccBimAttributeUser + `"
		  password = "` + testBimUserPassword + `"
		  email = "` + testBimUserEmail + `"
		  snowflake_user = "` + "user_attribute_test_sf_user" + `"	
	}

    resource "immuta_bim_attribute" "test" {
			iam_id = "bim"
			model_type = "user"
			model_id = immuta_bim_user.test.id
			key = "` + key + `"
			value = "` + value + `"
	}
`
}
