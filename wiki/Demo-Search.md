# Demo: Search

Index packs, local search, recommend shortlist, and inspect a card.

![Search demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/search.gif)

## Try it yourself

### 1. Packs and index

```bash
runhug packs list
runhug packs install          # if anything is missing
runhug index
```

### 2. Local search

```bash
runhug search -q 'small instruct' --limit 5
runhug search aero --type llm --limit 5
```

### 3. Recommend and size

```bash
runhug recommend 'small instruct for a laptop' --no-llm --candidates 5
runhug recommend gpu Qwen/Qwen2.5-0.5B-Instruct
```

### 4. Inspect a model

```bash
runhug inspect Qwen/Qwen2.5-0.5B-Instruct
```

← [All demos](Demos) · [Setup](Setup)
