package immuta

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/numberplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"math/big"
	"reflect"
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

func goMapFromTf(ctx context.Context, m types.Map) (map[string]interface{}, diag.Diagnostics) {
	goObject := make(map[string]interface{})
	// read from the terraform data into the map
	if diags := m.ElementsAs(ctx, &goObject, false); diags != nil {
		return nil, diags
	}
	return goObject, nil
}

func tfMapFromGo(ctx context.Context, m map[string]interface{}) (types.Map, diag.Diagnostics) {
	mappedValue, diags := types.MapValueFrom(ctx, types.StringType, m)
	if diags != nil {
		return types.MapNull(nil), diags
	}
	return mappedValue, nil
}

func checkIfMapHasChanged(ctx context.Context, tfMap types.Map, comparisonMap map[string]interface{}) (types.Map, diag.Diagnostics) {

	goTfMap, diags := goMapFromTf(ctx, tfMap)
	if diags != nil {
		return types.MapNull(nil), diags
	}

	// compare the map to the response from the API and if it's changed update the data object
	if !reflect.DeepEqual(goTfMap, comparisonMap) {
		tfFromComparison, err := tfMapFromGo(ctx, comparisonMap)
		if err != nil {
			return types.MapNull(nil), err
		}
		return tfFromComparison, nil
	}
	//return types.MapNull(nil), nil
	return tfMap, nil
}

func goListFromTf(ctx context.Context, l types.List) ([]interface{}, diag.Diagnostics) {
	goObject := make([]interface{}, 0)
	// read from the terraform data into the map
	if diags := l.ElementsAs(ctx, &goObject, false); diags != nil {
		return nil, diags
	}
	return goObject, nil
}

func tfListFromGo(ctx context.Context, l []interface{}) (types.List, diag.Diagnostics) {
	mappedValue, diags := types.ListValueFrom(ctx, types.StringType, l)
	if diags != nil {
		return types.ListNull(nil), diags
	}
	return mappedValue, nil
}

func checkIfListHasChanged(ctx context.Context, tfList types.List, comparisonList []interface{}) (types.List, diag.Diagnostics) {

	goTfList, diags := goListFromTf(ctx, tfList)
	if diags != nil {
		return types.ListNull(nil), diags
	}

	// compare two lists to see if they are equal

	listsAreSame := true
	if len(goTfList) != len(comparisonList) {
		listsAreSame = false
	}
	for i := range goTfList {
		if goTfList[i] != comparisonList[i] {
			listsAreSame = false
		}
	}

	if !listsAreSame {
		tfFromComparison, comparisonDiags := tfListFromGo(ctx, comparisonList)
		if comparisonDiags != nil {
			return types.ListNull(nil), comparisonDiags
		}
		return tfFromComparison, nil
	}
	//return types.ListNull(nil), nil
	return tfList, nil
}
