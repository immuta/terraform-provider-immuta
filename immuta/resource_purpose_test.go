package immuta

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"testing"
)

var testResourceName = "[Test] Terraform acc test"
var testResourceDescription = "A purpose created by a Terraform acceptance test"
var testResourceAcknowledgement = "I will not use this purpose as it is a test artifact"

func TestAccPurpose_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories(&testAccProviders),
		CheckDestroy:      testAccCheckPurposeDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPurposeConfig(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "name", testResourceName),
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "description", testResourceDescription),
					resource.TestCheckResourceAttr(
						"immuta_purpose.test", "acknowledgement", testResourceAcknowledgement),
				),
			},
		},
	})
}

func testAccPreCheck(t *testing.T)                        {}
func testAccCheckPurposeDestroy(s *terraform.State) error { return nil }

func testAccPurposeConfig() string {
	return fmt.Sprintf(`
	resource "immuta_purpose" "test" {
		  name        = "%[1]s"
		  description = "%[2]s"
		  acknowledgement = "%[3]s"
	}
`, testResourceName, testResourceDescription, testResourceAcknowledgement)
}
