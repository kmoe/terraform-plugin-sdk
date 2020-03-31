package ctytest

// TODO should this just be acctest package?

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/hashicorp/terraform-plugin-sdk/internal/configs/configschema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
	"github.com/zclconf/go-cty/cty"
)

type CtyChecker struct {
	schemas configschema.Block
}

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
	// recursion

	block := configschema.Block{}

	// attributes
	for attributeName, attributeValue := range jsonBlock.Attributes {
		block.Attributes[attributeName] = shimJsonSchemaAttribute(attributeValue)
	}

	return &block

	// TODO BLOCKS
}

// func shimJsonSchema(jsonSchema *tfjson.Schema) map[string]*configschema.Block {

// }

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

func CtyCheck(legacyState *terraform.State, provider terraform.ResourceProvider, checkFunc CtyCheckFunc) error {
	// shim to the new state format
	state, err := terraform.ShimLegacyState(legacyState)
	if err != nil {
		panic(err)
	}

	schemaRequest := terraform.ProviderSchemaRequest{
		ResourceTypes: []string{"random_string"},
	}

	schema, err := provider.GetSchema(&schemaRequest)
	if err != nil {
		panic(err)
	}

	resSchema := schema.ResourceTypes["random_string"]

	path := cty.GetAttrPath("length")

	ms := state.RootModule()
	res := ms.Resources["random_string.foo"]

	for _, is := range res.Instances {
		if is.HasCurrent() {
			resInstObjSrc := is.Current

			obj, err := resInstObjSrc.Decode(resSchema.ImpliedType())
			if err != nil {
				panic(err)
			}

			val, err := path.Apply(obj.Value)
			if err != nil {
				panic(err)
			}
			fmt.Print(val)

			err = checkFunc(val)
			if err != nil {
				return err
			}

			ret = fmt.Sprintf("VAL: %s", val)

		}
	}

	return nil
}

// ComposeAggregateCheckFunc lets you compose multiple CtyCheckFuncs into
// a single CtyCheckFunc.
//
// Unlike ComposeCheckFunc, ComposeAggergateCheckFunc runs _all_ of the
// CtyCheckFuncs and aggregates failures.
func ComposeAggregateCheckFunc(fs ...CtyCheckFunc) CtyCheckFunc {
	return func(s cty.Value) error {
		var result *multierror.Error

		for i, f := range fs {
			if err := f(s); err != nil {
				result = multierror.Append(result, fmt.Errorf("Check %d/%d error: %s", i+1, len(fs), err))
			}
		}

		return result.ErrorOrNil()
	}

}

// ComposeCheckFunc lets you compose multiple CtyCheckFuncs into
// a single CtyCheckFunc.
func ComposeCheckFunc(fs ...CtyCheckFunc) CtyCheckFunc {
	return func(s cty.Value) error {
		for i, f := range fs {
			if err := f(s); err != nil {
				return fmt.Errorf("Check %d/%d error: %s", i+1, len(fs), err)
			}
		}

		return nil
	}
}

// func AttributeIsNull(address string, path cty.Path) bool {
// 	// resolve the path
// 	// do the cty.IsNull check

// }

func AttributeIsNotNull(address string, path cty.Path) {}

func AttributeEquals(address string, path cty.Path, value cty.Value) {}

func AttributeEqualsToPtr(address string, path cty.Path, value *cty.Value) {}

func AttributesEqual(leftAddress string, leftPath cty.Path,
	rightAddress string, rightPath cty.Path) {
}

func StringEquals(address string, path cty.Path, value string) {}

func StringMatchFunc(address string, path cty.Path, matchFunc func(string) error) {}

func IntEquals(address string, path cty.Path, value int) {}

func FloatEquals(address string, path cty.Path, value float64) {}

func IsTrue(address string, path cty.Path) {}

func IsFalse(address string, path cty.Path) {}

func SetLengthEquals(address string, path cty.Path, length int) {}

func SetEquals(address string, path cty.Path, values []cty.Value) {}

func ListLengthEquals(address string, path cty.Path, length int) {}

func ListEquals(address string, path cty.Path, values []cty.Value) {}

func MapLengthEquals(address string, path cty.Path, length int) {}

func MapEquals(address string, path cty.Path, m map[string]cty.Value) {}
