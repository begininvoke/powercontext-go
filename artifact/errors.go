package artifact

import "fmt"

type NotFoundError struct{ Ref Ref }

func (e *NotFoundError) Error() string { return "artifact was not found" }

type InvalidReferenceError struct {
	Field  string
	Detail string
}

func (e *InvalidReferenceError) Error() string {
	return fmt.Sprintf("invalid Artifact reference %s: %s", e.Field, e.Detail)
}

type FamilyMismatchError struct {
	ArtifactFamily string
	DraftFamily    string
}

func (e *FamilyMismatchError) Error() string { return "artifact and draft families do not match" }

type RevisionConflictError struct {
	Requested Ref
	Current   Ref
}

func (e *RevisionConflictError) Error() string { return "artifact is not the latest revision" }
