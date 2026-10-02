# Demo: Search

Index packs, local search, recommend shortlist, and inspect a card.

![Search demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/search.gif)

**Tape source:** [`assets/search.tape`](https://github.com/openhat-security/runhug/blob/main/assets/search.tape) · **MP4:** [`search.mp4`](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/search.mp4)

## Re-record

```bash
make demo-vhs TAPE=search
```

## Commands shown

```bash
runhug packs list
runhug index
runhug search -q 'small instruct' --limit 5
runhug search aero --type llm --limit 5
runhug recommend 'small instruct for a laptop' --no-llm --candidates 5
runhug recommend gpu Qwen/Qwen2.5-0.5B-Instruct
runhug inspect Qwen/Qwen2.5-0.5B-Instruct
```

← [All demos](Demos) · [Setup](Setup)
