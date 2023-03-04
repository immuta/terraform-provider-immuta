package immuta

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"testing"
)

const testProjectName = "[TF Test] Terraform acc test"
const testProjectDocumentation = "Test documentation"
const testProjectKey = "test-project-key"

func TestAccProject_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckProjectDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccProjectConfig("desca"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_project.test", "name", testProjectName),
					resource.TestCheckResourceAttr(
						"immuta_project.test", "description", ("desca")),
					resource.TestCheckResourceAttr(
						"immuta_project.test", "documentation", testProjectDocumentation),
					resource.TestCheckResourceAttr(
						"immuta_project.test", "project_key", testProjectKey),
				),
			},
			// test update and read
			{
				Config: testAccProjectConfig("descb"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_project.test", "name", testProjectName),
					resource.TestCheckResourceAttr(
						"immuta_project.test", "description", "descb"),
					resource.TestCheckResourceAttr(
						"immuta_project.test", "documentation", testProjectDocumentation),
					resource.TestCheckResourceAttr(
						"immuta_project.test", "project_key", testProjectKey),
				),
			},
		},
	})
}

// todo this should ensure that the resource has actually been destroyed
func testAccCheckProjectDestroy(s *terraform.State) error {
	return nil
}

func testAccProjectConfig(desc string) string {
	return `
	resource "immuta_project" "test" {
		  name        = "` + testProjectName + `"
		  description = "` + desc + `"
		  documentation = "` + testProjectDocumentation + `"
		  project_key = "` + testProjectKey + `"
	}
`
}
