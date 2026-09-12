package validator

import (
	"context"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/satorunooshie/jsonschema-projector/internal/loader"
	"github.com/satorunooshie/jsonschema-projector/projector"
)

const defaultResourceURL = "projected.schema.json"

func CompileSchema(ctx context.Context, schema map[string]any, cfg projector.ValidationConfig, resourceURL string) (projector.Diagnostics, error) {
	if !cfg.Schema {
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	draft, err := draftForName(cfg.DefaultDraft)
	if err != nil {
		return projector.Diagnostics{{
			Severity: projector.SeverityError,
			Code:     projector.CodeInvalidValidateConfig,
			Message:  err.Error(),
			Pointer:  "validate.defaultDraft",
		}}, nil
	}

	c := jsonschema.NewCompiler()
	c.DefaultDraft(draft)
	c.UseLoader(jsonschema.SchemeURLLoader{
		"file": jsonschema.FileLoader{},
	})
	if cfg.AssertFormat {
		c.AssertFormat()
	}
	if cfg.AssertContent {
		c.AssertContent()
	}

	if resourceURL == "" || resourceURL == "-" {
		resourceURL = defaultResourceURL
	}

	if err := c.AddResource(resourceURL, loader.DeepCopyObject(schema)); err != nil {
		return projector.Diagnostics{compileFailed(resourceURL, err)}, nil
	}
	if _, err := c.Compile(resourceURL); err != nil {
		return projector.Diagnostics{compileFailed(resourceURL, err)}, nil
	}

	return nil, ctx.Err()
}

func draftForName(name string) (*jsonschema.Draft, error) {
	switch name {
	case "", "2020", "2020-12", "draft2020", "draft2020-12":
		return jsonschema.Draft2020, nil
	case "2019", "2019-09", "draft2019", "draft2019-09":
		return jsonschema.Draft2019, nil
	case "7", "07", "draft7", "draft-07":
		return jsonschema.Draft7, nil
	case "6", "06", "draft6", "draft-06":
		return jsonschema.Draft6, nil
	case "4", "04", "draft4", "draft-04":
		return jsonschema.Draft4, nil
	default:
		return nil, fmt.Errorf("unsupported default draft %q", name)
	}
}

func compileFailed(resourceURL string, err error) projector.Diagnostic {
	return projector.Diagnostic{
		Severity: projector.SeverityError,
		Code:     projector.CodeSchemaCompileFailed,
		Message:  fmt.Sprintf("projected schema does not compile: %v", err),
		Pointer:  "validate.schema",
		Ref:      resourceURL,
		Hint:     "fix the projected schema or adjust project.includeDefinitions and project.rewriteRefs before generating Go types",
	}
}
