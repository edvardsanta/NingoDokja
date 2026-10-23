# Bounded inference contract

This domain module validates provider-independent proposals, evidence citations,
state revisions and candidate actions. It does not invoke providers, schedule
retrieval, mutate memory or authorize execution. It is a library, not a service.

Version `inference.v1` is experimental and deliberately scoped to the synthetic
sum simulation: boolean evidence, simulation episodes and the
`simulated_sum_success` target. Do not treat it as a production inference API.
Provider probabilities are uncalibrated hypothetical estimates.

Run `go test -race ./...` and `go vet ./...` in this directory.
See `dokja_docs/model-assisted-cognition-experiment.md` for integration and limits.

`SchemaFor` grounds output choices in each request. `required_facts` exposes
execution prerequisites. Optional episode diagnostics describe observed parser
or sum results; they are not model recommendations or evidence of future success.
