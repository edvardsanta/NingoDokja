# Domain Responsibilities

This document explains what belongs in the Dokja domain layer and what does not.

Target flow:

`Interface -> Orchestrator -> Domain -> Service`

The domain layer is where business decisions live.

## Responsibility Split

### Interfaces

Interfaces are responsible for:

- receiving external input
- converting input into events or requests
- sending those events to the orchestrator
- rendering orchestrator responses back to users

Interfaces must not:

- call `dokja_services` directly
- decide workflows
- own business rules

### Orchestrator

The orchestrator is responsible for:

- routing events
- building workflow context
- planning workflow steps
- dispatching to domains
- coordinating cross-domain or cross-interface workflows

The orchestrator must not:

- contain domain-specific business rules
- become the service integration layer

### Domains

Domains are responsible for:

- business decisions
- action interpretation
- payload normalization related to business logic
- deciding which service operation should run
- enforcing domain-specific rules

Domains should depend on small service or policy ports.

Domains should not:

- know transport details like Discord or ZeroMQ
- own infrastructure protocols
- become interface adapters

### Services

Services are responsible for:

- stateless external integrations
- infrastructure access
- HTTP/API/DB/LLM calls
- scraping, storage, remote service I/O

Services should not:

- decide workflows
- embed domain policy

## Current Domains

Current domain modules:

- [dokja_book](../dokja_domain/dokja_book)
- [dokja_chat](../dokja_domain/dokja_chat)
- [dokja_meme](../dokja_domain/dokja_meme)
- [dokja_moderation](../dokja_domain/dokja_moderation)

## Book Domain

Location:

- [domain.go](../dokja_domain/dokja_book/domain.go)

Owns:

- interpreting book workflow actions
- deciding whether a request is classification-only or summarization
- merging workflow payload/context into a domain input contract
- keeping book summarization concerns out of generic chat flows

Depends on ports:

- book service integration port

Does not own:

- raw PDF / EPUB / MOBI parser implementation details
- HTTP serving
- interface-specific upload behavior

## Chat Domain

Location:

- [domain.py](../dokja_domain/dokja_chat/domain.py)

Owns:

- validating input message content
- cache lookup decision
- deciding whether to return cached output or call AI
- parsing AI response into domain response structure
- deciding what should be persisted

Depends on ports:

- repository port
- AI service port

Does not own:

- Discord message handling
- HTTP serving
- raw OpenAI client wiring

## Meme Domain

Location:

- [domain.go](../dokja_domain/dokja_meme/domain.go)

Owns:

- mapping meme actions to service operations
- interpreting domain payload values like `limit` and `max_items_per_scraper`
- deciding which meme service operation applies to the workflow action

Depends on ports:

- meme service integration port

Does not own:

- scheduler timing
- Discord delivery
- raw scraper/database/network details

## Moderation Domain

Location:

- [domain.go](../dokja_domain/dokja_moderation/domain.go)

Owns:

- `screen-event` business action
- extracting textual content from supported payload keys
- deciding whether moderation should allow, block, or skip
- returning structured moderation results

Depends on ports:

- moderation policy port

Does not own:

- chat generation
- Discord transport behavior
- concrete moderation provider implementation unless injected as a policy

## Good Examples

Good domain logic:

- "if there is a cached chat response, return it"
- "for `meme.pool.refresh`, use the refresh operation with a default max value"
- "for incoming messages, moderation should inspect textual content before chat runs"

## Bad Examples

Bad domain placement:

- Discord slash-command parsing inside a domain
- ZeroMQ request handling inside a domain
- HTTP route handlers deciding which workflow to execute
- a service client deciding whether an event should be blocked

## Practical Rule

When deciding where new code belongs:

- if it decides behavior, it probably belongs in a domain
- if it routes work between domains, it belongs in the orchestrator
- if it talks to Discord, OpenAI, SQLite, HTTP APIs, or scraping targets, it belongs in a service or interface
