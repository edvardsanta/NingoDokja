module read_books/dokja_orch/runtime

go 1.25.0

require (
 read_books/dokja_domain/dokja_runtime v0.0.0
 read_books/dokja_services/dokja_runtime v0.0.0
)

replace read_books/dokja_domain/dokja_runtime => ../../dokja_domain/dokja_runtime
replace read_books/dokja_services/dokja_runtime => ../../dokja_services/dokja_runtime

require github.com/pebbe/zmq4 v1.4.0 // indirect
