package ctytest

// TODO should this just be acctest package?

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/hashicorp/terraform-plugin-sdk/internal/addrs"
	"github.com/hashicorp/terraform-plugin-sdk/internal/configs/configschema"
	"github.com/hashicorp/terraform-plugin-sdk/internal/states"
	"github.com/hashicorp/terraform-plugin-sdk/internal/tfdiags"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
	"github.com/zclconf/go-cty/cty"
)

type CtyChecker struct {
	// schemas configschema.Block
	provider *terraform.ResourceProvider
}

type CtyStateCheckFunc func(*terraform.State, *terraform.ResourceProvider) error

type CtyCheckFunc func(cty.Value) error

func shimJsonSchemaAttribute(jsonAttribute *tfjson.SchemaAttribute) *configschema.Attribute {
	attribute := configschema.Attribute{
		Type:            jsonAttribute.AttributeType,
		Description:     jsonAttribute.Description,
		DescriptionKind: configschema.StringPlain,
		Required:        jsonAttribute.Required,
		Optional:        jsonAttribute.Optional,
		Computed:        jsonAttribute.Computed,
		Sensitive:       jsonAttribute.Sensitive,
	}
	return &attribute
}

func shimJsonSchemaBlock(jsonBlock tfjson.SchemaBlock) *configschema.Block {
	block := configschema.Block{}

	for attributeName, attributeValue := range jsonBlock.Attributes {
		block.Attributes[attributeName] = shimJsonSchemaAttribute(attributeValue)
	}

	return &block

	// TODO KEM BLOCK RECURSION
}

func shimJsonProviderSchema(jsonProviderSchema *tfjson.ProviderSchema) *terraform.ProviderSchema {
	resourceTypes := map[string]*configschema.Block{}

	for resourceName, resourceSchema := range jsonProviderSchema.ResourceSchemas {
		shimmedResourceSchema := shimJsonSchemaBlock(*resourceSchema.Block)
		resourceTypes[resourceName] = shimmedResourceSchema
	}

	providerSchema := terraform.ProviderSchema{
		ResourceTypes: resourceTypes,
	}

	return &providerSchema
}

func shimSchemasFromJson(jsonSchema *tfjson.ProviderSchemas) *terraform.Schemas {
	schemas := new(terraform.Schemas)

	for providerName, providerSchema := range jsonSchema.Schemas {

		schemas.Providers[providerName] = shimJsonProviderSchema(providerSchema)
	}

	return schemas
}

// ComposeAggregateCheckFunc lets you compose multiple CtyCheckFuncs into
// a single CtyCheckFunc.
//
// Unlike ComposeCheckFunc, ComposeAggergateCheckFunc runs _all_ of the
// CtyCheckFuncs and aggregates failures.
func ComposeAggregateCheckFunc(fs ...CtyCheckFunc) CtyCheckFunc {
	return func(v cty.Value) error {
		var result *multierror.Error

		for i, f := range fs {
			if err := f(v); err != nil {
				result = multierror.Append(result, fmt.Errorf("Check %d/%d error: %s", i+1, len(fs), err))
			}
		}

		return result.ErrorOrNil()
	}

}

// ComposeCheckFunc lets you compose multiple CtyCheckFuncs into
// a single CtyCheckFunc.
func ComposeCheckFunc(fs ...CtyCheckFunc) CtyCheckFunc {
	return func(v cty.Value) error {
		for i, f := range fs {
			if err := f(v); err != nil {
				return fmt.Errorf("Check %d/%d error: %s", i+1, len(fs), err)
			}
		}

		return nil
	}
}

func parseResourceAddr(resourceAddr string) (*addrs.AbsResource, error) {
	traversal, travDiags := hclsyntax.ParseTraversalAbs([]byte(resourceAddr), "", hcl.Pos{Line: 1, Column: 1})
	if travDiags.HasErrors() {
		return nil, travDiags.Error()
	}

	r, diags := addrs.ParseTarget(traversal)

	if diags.HasErrors() {
		return nil, diags.Error()
	}

	return *r.Resource, nil
}

func ctyCheck(legacyState *terraform.State, provider terraform.ResourceProvider, name string, path cty.Path, checkFunc CtyCheckFunc) (*states.Resource, error) {
	// shim to the new state format
	state, err := terraform.ShimLegacyState(legacyState)
	if err != nil {
		return nil, err
	}

	r, err := parseResourceAddr(name)
	if err != nil {
		return nil, err
	}

	var schemaRequest terraform.ProviderSchemaRequest

	if r.Mode == addrs.ManagedResourceMode {
		schemaRequest = terraform.ProviderSchemaRequest{
			ResourceTypes: []string{r.Resource.Type},
		}
	} else { // assume addrs.DataResourceMode
		schemaRequest = terraform.ProviderSchemaRequest{
			DataSources: []string{r.Resource.Type},
		}
	}

	// TODO KEM WHAT IF IT'S A DATA SOURCE???

	schema, err := provider.GetSchema(&schemaRequest)
	if err != nil {
		return nil, err
	}

	resSchema := schema.ResourceTypes[r.Resource.Type]

	ms := state.RootModule()
	res := ms.Resources[name]

	for _, is := range res.Instances {
		if is.HasCurrent() {
			resInstObjSrc := is.Current

			obj, err := resInstObjSrc.Decode(resSchema.ImpliedType())
			if err != nil {
				return err
			}

			val, err := path.Apply(obj.Value)
			if err != nil {
				return err
			}
			fmt.Print(val)

			err = checkFunc(val)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func attributeIsNotNull(v cty.Value) CtyCheckFunc {
	return func(v cty.Value) error {
		ok := v.IsNull()
		if !ok {
			return fmt.Errorf("expected %s to be null, but it was non-null", v)
		}
		return nil
	}
}

func AttributeIsNotNull(address string, path cty.Path) CtyStateCheckFunc {
	return func(state *terraform.State, provider *terraform.ResourceProvider) error {
		return ctyCheck(state, provider, address, path, attributeIsNotNull)

	}
}

func attributeEquals(other cty.Value) CtyCheckFunc {
	return func(v cty.Value) error {
		ok := v.Equals(other)
		if !ok {
			return fmt.Errorf("expected %s to equal %s", v, other)
		}
	}
}

func AttributeEquals(address string, path cty.Path, value cty.Value) {
	return func(state *terraform.State, provider *terraform.ResourceProvider) error {
		return ctyCheck(state, provider, address, path, attributeEquals(value))
	}
}

// TODO KEM
// func attributeEqualsToPtr(v cty.Value, other *ctyValue) bool {
// }
// func AttributeEqualsToPtr(address string, path cty.Path, value *cty.Value) {}

// func AttributesEqual(leftAddress string, leftPath cty.Path,
// 	rightAddress string, rightPath cty.Path) {
// }

func stringEquals(value string) CtyCheckFunc {
	return func(v cty.Value) {
		s := cty.StringVal(value)
		return v.Equals(s)
	}
}

func StringEquals(address string, path cty.Path, value string) CtyStateCheckFunc {
	return func(state *terraform.State, provider *terraform.ResourceProvider) error {
		return ctyCheck(state, provider, address, path, stringEquals(value))
	}
}

func stringMatchFunc(matchFunc func(string) error) CtyCheckFunc {
	return func(v cty.Value) {
		s := v.String()
		return matchFunc(s)
	}
}

func StringMatchFunc(address string, path cty.Path, matchFunc func(string) error) CtyStateCheckFunc {
	return func(state *terraform.State, provider *terraform.ResourceProvider) error {
		return ctyCheck(state, provider, address, path, stringMatchFunc())
	}
}

func intEquals(value int) CtyCheckFunc {
	i := cty.NumberIntVal(i)
	return func(v cty.Value) error {
		return v.Equals(i)
	}
}

func IntEquals(address string, path cty.Path, value int) CtyStateCheckFunc {
	return func(state *terraform.State, provider *terraform.ResourceProvider) error {
		return ctyCheck(state, provider, address, path, intEquals(value))
	}
}

func floatEquals(value float64) CtyCheckFunc {
	f := cty.NumberFloatVal(value)
	return func(v cty.Value) error {
		return v.Equals(f)
	}
}

func FloatEquals(address string, path cty.Path, value float64) CtyStateCheckFunc {
	return func(state *terraform.State, provider *terraform.ResourceProvider) error {
		return ctyCheck(state, provider, address, path, floatEquals(value))
	}
}

func isTrue() CtyCheckFunc {
	return func(v cty.Value) error {
		return v.Equals(cty.True)
	}
}

func IsTrue(address string, path cty.Path) CtyStateCheckFunc {
	return func(state *terraform.State, provider *terraform.ResourceProvider) error {
		return ctyCheck(state, provider, address, path, isTrue())
	}
}

func isFalse() CtyCheckFunc {
	return func(v cty.Value) error {
		return v.Equals(cty.False)
	}
}

func IsFalse(address string, path cty.Path) CtyStateCheckFunc {
	return func(state *terraform.State, provider *terraform.ResourceProvider) error {
		return ctyCheck(state, provider, address, path, isFalse())
	}
}

// TODO KEM
// func setLengthEquals(length int) CtyCheckFunc {
// 	return func(v cty.Value) error {

// 	}
// }

// func SetLengthEquals(address string, path cty.Path, length int) {}

// func SetEquals(address string, path cty.Path, values []cty.Value) {}

// func ListLengthEquals(address string, path cty.Path, length int) {}

// func ListEquals(address string, path cty.Path, values []cty.Value) {}

// func MapLengthEquals(address string, path cty.Path, length int) {}

// func MapEquals(address string, path cty.Path, m map[string]cty.Value) {}
