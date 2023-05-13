package immuta

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/numberplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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

func defaultToZeroValue() basetypes.ObjectAsOptions {
	return basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	}
}

func goMapFromTf[T any](ctx context.Context, m types.Map) (map[string]T, diag.Diagnostics) {
	goObject := make(map[string]T)
	// read from the terraform data into the map
	if diags := m.ElementsAs(ctx, &goObject, false); diags != nil {
		return nil, diags
	}
	return goObject, nil
}

func tfMapFromGo[T any](ctx context.Context, m map[string]T) (types.Map, diag.Diagnostics) {
	mappedValue, diags := types.MapValueFrom(ctx, types.StringType, m)
	if diags != nil {
		return types.MapNull(nil), diags
	}
	return mappedValue, nil
}

func updateMapIfChanged[T any](ctx context.Context, tfMap types.Map, comparisonMap map[string]T) (types.Map, diag.Diagnostics) {

	goTfMap, diags := goMapFromTf[T](ctx, tfMap)
	if diags != nil {
		return types.MapNull(nil), diags
	}

	// compare the map to the response from the API and if it's changed update the data object
	if !reflect.DeepEqual(goTfMap, comparisonMap) {
		tfFromComparison, err := tfMapFromGo[T](ctx, comparisonMap)
		if err != nil {
			return types.MapNull(nil), err
		}
		return tfFromComparison, nil
	}
	//return types.MapNull(nil), nil
	return tfMap, nil
}

func goListFromTf[T any](ctx context.Context, l types.List) ([]T, diag.Diagnostics) {
	goObject := make([]T, 0)
	// read from the terraform data into the map
	if diags := l.ElementsAs(ctx, &goObject, false); diags != nil {
		return nil, diags
	}
	return goObject, nil
}

func tfListFromGo[T any](ctx context.Context, l []T) (types.List, diag.Diagnostics) {
	mappedValue, diags := types.ListValueFrom(ctx, types.StringType, l)
	if diags != nil {
		return types.ListNull(nil), diags
	}
	return mappedValue, nil
}

func updateListIfChanged[T any](ctx context.Context, tfList types.List, comparisonList []T) (types.List, diag.Diagnostics) {

	goTfList, diags := goListFromTf[T](ctx, tfList)
	if diags != nil {
		return types.ListNull(nil), diags
	}

	// compare two lists to see if they are equal

	//listsAreSame := true
	//if len(goTfList) != len(comparisonList) {
	//	listsAreSame = false
	//}
	//for i := range goTfList {
	//	if goTfList[i] != comparisonList[i] {
	//		listsAreSame = false
	//	}
	//}

	if !reflect.DeepEqual(goTfList, comparisonList) {
		tfFromComparison, comparisonDiags := tfListFromGo[T](ctx, comparisonList)
		if comparisonDiags != nil {
			return types.ListNull(nil), comparisonDiags
		}
		return tfFromComparison, nil
	}
	//return types.ListNull(nil), nil
	return tfList, nil
}
