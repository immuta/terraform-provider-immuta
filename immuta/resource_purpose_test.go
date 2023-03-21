package immuta

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"testing"
)

const testResourceName = "[TF Test] Terraform acc test"
const testResourceDescription = "A purpose created by a Terraform acceptance test"
const testResourceAcknowledgement = "I will not use this purpose as it is a test artifact"

func TestAccPurpose_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckPurposeDestroy,
		Steps: []resource.TestStep{
			// test create and read
			{
				Config: testAccPurposeConfig("a"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "name", testResourceName),
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "description", testResourceDescription+" a"),
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "acknowledgement", testResourceAcknowledgement),
				),
			},
			// test update and read
			{
				Config: testAccPurposeConfig("b"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "name", testResourceName),
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "description", testResourceDescription+" b"),
				),
			},
		},
	})
}

// todo this should ensure that the resource has actually been destroyed
func testAccCheckPurposeDestroy(s *terraform.State) error { return nil }

func testAccPurposeConfig(descriptionAppend string) string {
	return fmt.Sprintf(`
	resource "immuta_purpose" "test" {
		  name        = "%[1]s"
		  description = "%[2]s %[3]s"
		  acknowledgement = "%[4]s"
	}
`, testResourceName, testResourceDescription, descriptionAppend, testResourceAcknowledgement)
}

func TestAccPurpose_noOptionalParams(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckPurposeDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPurposeConfigNoOptionalParams(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "name", testResourceName),
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "description", testResourceDescription),
				),
			},
		},
	})
}

func testAccPurposeConfigNoOptionalParams() string {
	return fmt.Sprintf(`
	resource "immuta_purpose" "test" {
		  name        = "%[1]s"
		  description = "%[2]s"
	}
`, testResourceName, testResourceDescription)
}
