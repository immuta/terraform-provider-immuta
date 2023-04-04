package immuta

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"testing"
)

const testTagPrefix = "TF_ACC_TEST_"

func TestAccTag_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckTagDestroy,
		Steps: []resource.TestStep{
			// test create and read
			{
				Config: testAccTagConfig("a"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_tag.test", "name", testTagPrefix+"a"),
				),
			},
			{
				Config: testAccTagConfigWithRoot("b", "a"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_tag.test_with_root", "name", fullTagName("b", "a")),
				),
			},
		},
	})
}

func testAccCheckTagDestroy(_ *terraform.State) error { return nil }

func testAccTagConfig(tagName string) string {
	return fmt.Sprintf(`
	resource "immuta_tag" "test" {
		name = "%s"
	}
`, testTagPrefix+tagName)
}

func testAccTagConfigWithRoot(tagName string, rootTag string) string {
	return fmt.Sprintf(`
	resource "immuta_tag" "test_with_root" {
		name = "%s"
		root_tag = "%s"
	}
`, fullTagName(tagName, rootTag), testTagPrefix+rootTag)
}

func fullTagName(tagName string, rootTag string) string {
	return testTagPrefix + rootTag + "." + testTagPrefix + tagName
}
