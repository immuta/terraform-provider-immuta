package immuta

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/numberplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"math/big"
)

func stringResourceId() schema.StringAttribute {
	return schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: "Terraform resource identifier",
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.UseStateForUnknown(),
		},
	}
}

func numberResourceId() schema.NumberAttribute {
	return schema.NumberAttribute{
		Computed:            true,
		MarkdownDescription: "Terraform resource identifier",
		PlanModifiers: []planmodifier.Number{
			numberplanmodifier.UseStateForUnknown(),
		},
	}
}

func intToNumberValue(i int) types.Number {
	return types.NumberValue(big.NewFloat(float64(i)))
}
