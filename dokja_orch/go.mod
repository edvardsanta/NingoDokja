module read_books

go 1.25.0

require read_books/dokja_domain/dokja_meme v0.0.0

require read_books/dokja_domain/dokja_moderation v0.0.0

require read_books/dokja_domain/dokja_book v0.0.0

require read_books/dokja_domain/dokja_knowledge v0.0.0

require read_books/dokja_store v0.0.0

replace read_books/dokja_domain/dokja_book => ../dokja_domain/dokja_book

replace read_books/dokja_domain/dokja_knowledge => ../dokja_domain/dokja_knowledge

replace read_books/dokja_domain/dokja_meme => ../dokja_domain/dokja_meme

replace read_books/dokja_domain/dokja_moderation => ../dokja_domain/dokja_moderation

replace read_books/dokja_store => ../dokja_store

require (
	github.com/gofiber/fiber/v2 v2.52.12
	github.com/google/uuid v1.6.0
	github.com/pebbe/zmq4 v1.4.0
)

require (
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.51.0 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.59.0 // indirect
)

require golang.org/x/sys v0.47.0 // indirect
