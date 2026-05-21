# Ningo Dokja (The Arrogant Philosopher)

> "A virtual assistant that doesn't just summarize your books, but judges them with the weight of an 'arrogant philosopher'."

Welcome to **Ningo Dokja**, a modular AI virtual assistant born from the intersection of Data Science and the mythos of *Omniscient Reader*. Originally conceived as a simple tool for book summarization, Dokja has evolved into a multi-interface presence—an opinionated, high-functioning digital entity designed to assist with research, generate creative content, and dominate social interactions with intellectual flair.

## The Vision

Dokja is more than a chatbot; it is an experiment in digital personality and utility. Built as part of a Data Science exploration, it bridges the gap between raw information processing and social engagement. Whether it's analyzing a complex dataset, summarizing a novel, or generating the perfect meme for your Discord server, Dokja does so with a distinct persona—talkative, well-read, and charmingly arrogant.

> **💡 Fun Fact: The Meaning of "Dokja"**
>
> In Korean, the name **Dokja (독자)** carries a triple meaning that perfectly mirrors this project's evolution:
> 1. **Reader (讀者):** Reflecting its origins as a book-summarization tool.
> 2. **Only Child (獨子):** Representing its status as a unique, custom-built assistant.
> 3. **Alone/Independent (獨自):** Marking its transformation into an autonomous, "arrogant philosopher" persona that stands on its own.
>
> **The Real Irony:** Despite the name fitting perfectly, its choice was entirely serendipitous—i have never actually read *Omniscient Reader*. Much like the protagonist of that story, Dokja seems to have manifested its own destiny, evolving from a mere observer of stories into the independent architect of its own digital world.

## Core Pillars

Dokja's existence is sustained by three foundational capabilities:

### 🧠 Intellectual Superiority (Knowledge & Intelligence)
Dokja excels at transforming raw data into actionable knowledge. It classifies resources, produces structured summaries, and analyzes software releases, all while maintaining a critical, philosophical perspective.

### 🎨 Social Dominance (Connection & Engagement)
AI is best when it's social. Dokja powers natural, context-aware conversations and manages creative content like memes. It doesn't just "interact"—it commands the conversation, making group chats more informative and significantly more entertaining.

### 🔌 Ubiquitous Presence (Integration)
A true philosopher is always accessible. Through its modular architecture, Dokja provides a consistent experience across **Discord** (voice and chat), the **CLI** (for those who prefer the terminal's purity), and **Mobile** platforms.

---

## Architectural Philosophy

At its heart, Dokja follows a **Modular Orchestrator** philosophy. 

Instead of a monolithic bot, it is a distributed system where **Interfaces** emit events to a central **Orchestrator**, which then coordinates with specialized **Domains** and **Services**. This architecture ensures that Dokja is resilient, extensible, and capable of supporting any interface or AI model.

For a deep dive into the technical details, see the [Architecture Documentation](./dokja_docs/README.md).

---

## Quick Start

The fastest way to deploy the Dokja stack is using Docker:

```bash
# Clone the repository and move into the directory
git clone https://github.com/your-repo/read_books.git
cd read_books

# Start the development stack
docker compose -f docker-compose.dev.yml up -d
```

---

## Navigation Hub

Explore the technical foundations of the project:

- **[Architecture Guide](./dokja_docs/README.md)**: Deep dive into the system design.
- **[Coding Conventions (AGENTS.md)](./AGENTS.md)**: Guidelines for contributing and AI-assisted coding.
- **[Domain Responsibilities](./dokja_docs/domain-responsibilities.md)**: Understanding the business logic boundaries.
- **[Workflows](./dokja_docs/workflows.md)**: Step-by-step breakdowns of system operations.

---

*Built with ❤️ for a more intelligent and fun digital life.*
