package immuta

import (
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"os"
	"strings"
	"testing"
)

const testDataSourceDatabase = "TERRAFORM_INTEGRATION_TEST"
const testDataSourceSchema = "TEST_SCHEMA"
const testDataSourceConnectionKey = "tf-acc-test-connection"

func TestAccDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testAccDataSourcePreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDataSourceDestroy,
		Steps: []resource.TestStep{
			// test create and read
			{
				Config: testAccDataSourceConfig([]string{"a"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_data_source.test", "connection_key", testDataSourceConnectionKey),
				),
			},
			// test update and read
			{
				Config: testAccDataSourceConfig([]string{"a", "b"}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"immuta_data_source.test", "connection_key", testDataSourceConnectionKey),
					resource.TestCheckResourceAttr(
						"immuta_data_source.test", "options.table_tags.0", "a"),
					resource.TestCheckResourceAttr(
						"immuta_data_source.test", "options.table_tags.1", "b"),
				),
			},
		},
	})
}

func testAccDataSourcePreCheck(t *testing.T) {
	if os.Getenv("ACC_IMMUTA_SNOWFLAKE_USERNAME") == "" {
		t.Fatal("ACC_IMMUTA_SNOWFLAKE_USERNAME must be set for data source acceptance tests")
	}
	if os.Getenv("ACC_IMMUTA_SNOWFLAKE_PASSWORD") == "" {
		t.Fatal("ACC_IMMUTA_SNOWFLAKE_PASSWORD must be set for data source acceptance tests")
	}
	if os.Getenv("ACC_IMMUTA_SNOWFLAKE_HOST") == "" {
		t.Fatal("ACC_IMMUTA_SNOWFLAKE_HOST must be set for data source acceptance tests")
	}
	if os.Getenv("ACC_IMMUTA_SNOWFLAKE_WAREHOUSE") == "" {
		t.Fatal("ACC_IMMUTA_SNOWFLAKE_WAREHOUSE must be set for data source acceptance tests")
	}
	if os.Getenv("ACC_IMMUTA_SNOWFLAKE_ROLE") == "" {
		t.Fatal("ACC_IMMUTA_SNOWFLAKE_ROLE must be set for data source acceptance tests")
	}
}

func testAccCheckDataSourceDestroy(_ *terraform.State) error { return nil }

func testAccDataSourceConfig(tags []string) string {

	username := os.Getenv("ACC_IMMUTA_SNOWFLAKE_USERNAME")
	password := os.Getenv("ACC_IMMUTA_SNOWFLAKE_PASSWORD")
	host := os.Getenv("ACC_IMMUTA_SNOWFLAKE_HOST")
	warehouse := os.Getenv("ACC_IMMUTA_SNOWFLAKE_WAREHOUSE")
	role := os.Getenv("ACC_IMMUTA_SNOWFLAKE_ROLE")

	tags = append(tags, "terraform_integration_test")
	for index, tag := range tags {
		tags[index] = "\"" + tag + "\""
	}
	tagsString := "[" + strings.Join(tags, ", ") + "]"

	return fmt.Sprintf(`
	resource "immuta_data_source" "test" {
		connection_key = "%[9]s"
		name_template = {
			data_source_format = "tfacc::<DATABASE>.<SCHEMA>.<TABLENAME>"
			table_format = "tfacc_<database>_<schema>_<tablename>"
			schema_format = "tfacc_<database>_<schema>"
			schema_project_name_format = "tfacc::<database>.<schema>"
        }
		connection_details = {
			handler = "Snowflake"
			hostname = "%[1]s"
			port = 443
			database = "%[2]s"
			schema = "%[3]s"
			username = "%[4]s"
			password = "%[5]s"
			warehouse = "%[6]s"
			ssl = true
			connection_string_options = "role=%[7]s"
		}
		options = {
			table_tags = %[8]s
		}
	}`, host, testDataSourceDatabase, testDataSourceSchema, username, password, warehouse, role, tagsString, testDataSourceConnectionKey)
}
