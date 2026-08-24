package schemas_test

import (
	"testing"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/schemas"
)

func TestInspectionResultSchemaRejectsContractDrift(t *testing.T) {
	valid := inspector.PublicError("missing.bin", inspector.ModeStandard, 5000, "E_FILE_NOT_FOUND", "missing")
	if err := schemas.ValidateInspectionResult(valid); err != nil {
		t.Fatalf("valid public result: %v", err)
	}
	invalid := valid
	invalid.Status = "invented"
	if err := schemas.ValidateInspectionResult(invalid); err == nil {
		t.Fatal("schema accepted an undeclared result status")
	}
}
