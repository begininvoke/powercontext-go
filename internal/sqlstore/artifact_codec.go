package sqlstore

import (
	"fmt"
	"reflect"

	"github.com/thunguo/powercontext-go/artifact"
)

// ArtifactCodec is an exact family/content route for one persisted Artifact
// schema. It is immutable after construction.
type ArtifactCodec struct {
	family        string
	contentType   reflect.Type
	encode        func(any) ([]byte, error)
	decodeContent func([]byte) (any, error)
	decode        func(artifact.Ref, artifact.Lineage, []byte) (artifact.Snapshot, error)
}

// NewArtifactCodec constructs an exact concrete content codec.
func NewArtifactCodec[T any](
	family string,
	encode func(T) ([]byte, error),
	decode func([]byte) (T, error),
) (ArtifactCodec, error) {
	if _, err := artifact.NewRef(family, "codec-validation", 1); err != nil {
		return ArtifactCodec{}, err
	}
	contentType := reflect.TypeFor[T]()
	if contentType.Kind() == reflect.Interface {
		return ArtifactCodec{}, fmt.Errorf("sqlstore: Artifact codec content type must be concrete")
	}
	if encode == nil || decode == nil {
		return ArtifactCodec{}, fmt.Errorf("sqlstore: Artifact codec functions must not be nil")
	}
	return ArtifactCodec{
		family:      family,
		contentType: contentType,
		encode: func(value any) ([]byte, error) {
			if reflect.TypeOf(value) != contentType {
				return nil, fmt.Errorf("sqlstore: Artifact codec %q expected %s, got %T", family, contentType, value)
			}
			return encode(value.(T))
		},
		decodeContent: func(payload []byte) (any, error) {
			return decode(payload)
		},
		decode: func(ref artifact.Ref, lineage artifact.Lineage, payload []byte) (artifact.Snapshot, error) {
			content, err := decode(payload)
			if err != nil {
				return nil, err
			}
			return artifact.Restore(ref, content, lineage)
		},
	}, nil
}
