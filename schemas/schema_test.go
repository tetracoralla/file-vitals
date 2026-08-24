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

func TestCollectionResultSchemasRejectContractDrift(t *testing.T) {
	batch := inspector.PublicBatchError(5000, "E_INVALID_INPUT", "bad batch")
	if err := schemas.ValidateBatchResult(batch); err != nil {
		t.Fatalf("valid batch result: %v", err)
	}
	batch.Status = "invented"
	if err := schemas.ValidateBatchResult(batch); err == nil {
		t.Fatal("batch schema accepted an undeclared status")
	}

	inventory := inspector.PublicInventoryError(".", 4, 5000, "E_INVALID_INPUT", "bad root")
	if err := schemas.ValidateInventoryResult(inventory); err != nil {
		t.Fatalf("valid inventory result: %v", err)
	}
	inventory.FilesScanned = 33
	if err := schemas.ValidateInventoryResult(inventory); err == nil {
		t.Fatal("inventory schema accepted more than the public file limit")
	}
}
