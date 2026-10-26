module read_books/dokja_orch/runtime

go 1.25.0

require (
	read_books/dokja_domain/dokja_runtime v0.0.0
	read_books/dokja_services/dokja_runtime v0.0.0
)

replace read_books/dokja_domain/dokja_runtime => ../../dokja_domain/dokja_runtime

replace read_books/dokja_services/dokja_runtime => ../../dokja_services/dokja_runtime

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pebbe/zmq4 v1.4.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.59.0 // indirect
)

require (
	git.sr.ht/~sbinet/gg v0.5.0 // indirect
	github.com/campoy/embedmd v1.0.0 // indirect
	github.com/go-latex/latex v0.0.0-20250304174226-2790903426af // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/image v0.18.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	read_books/dokja_store v0.0.0 // indirect
)

replace read_books/dokja_store => ../../dokja_store
