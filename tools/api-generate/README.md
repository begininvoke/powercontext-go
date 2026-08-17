# API generator

Generation tooling for `api/v1` belongs here. Generated output derives only
from `openapi/powercontext.yaml`.

The frozen OpenAPI 3.0 document uses `$ref` with a `nullable: true` sibling.
OpenAPI 3.0 formally ignores `$ref` siblings, so the generator command creates
an ephemeral, semantically equivalent `oneOf: [$ref, null]` view before invoking
ogen. The canonical document is neither rewritten nor checked in twice.

The command also generates the PowerContext semantic validator for cross-field
Candidate evidence limits. Its model list is discovered from the canonical
schema using the same rule as the frozen Python generator, so this exception
to plain OpenAPI validation remains deterministic and reviewable.
