# Demos (VHS tapes)

Recorded with [charmbracelet/vhs](https://github.com/charmbracelet/vhs). Sources: `assets/*.tape` · GIFs: `assets/screenshots/`.

## Gallery

| Demo | Preview | Tape |
|------|---------|------|
| [Quickstart](Demo-Quickstart) | first-run story | [`quickstart.tape`](https://github.com/openhat-security/runhug/blob/main/assets/quickstart.tape) |
| [Search](Demo-Search) | packs → inspect | [`search.tape`](https://github.com/openhat-security/runhug/blob/main/assets/search.tape) |
| [Deploy](Demo-Deploy) | dry-run cloud | [`deploy.tape`](https://github.com/openhat-security/runhug/blob/main/assets/deploy.tape) |
| [Heretic](Demo-Heretic) | abliteration | [`heretic.tape`](https://github.com/openhat-security/runhug/blob/main/assets/heretic.tape) |
| [Run](Demo-Run) | local / chat / agents | [`run.tape`](https://github.com/openhat-security/runhug/blob/main/assets/run.tape) |
| [Full](Demo-Full) | everything in order | [`full.tape`](https://github.com/openhat-security/runhug/blob/main/assets/full.tape) |

<p>
<img src="https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/quickstart.gif" alt="quickstart" width="49%"/>
<img src="https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/search.gif" alt="search" width="49%"/>
</p>
<p>
<img src="https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/deploy.gif" alt="deploy" width="49%"/>
<img src="https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/heretic.gif" alt="heretic" width="49%"/>
</p>
<p>
<img src="https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/run.gif" alt="run" width="49%"/>
<img src="https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/runhug-demo.gif" alt="full" width="49%"/>
</p>

## Re-record

```bash
brew install vhs
git clone https://github.com/openhat-security/runhug.git && cd runhug
make demo-vhs TAPE=quickstart
make demo-vhs TAPE=full
make demo-vhs-all
```

Demos prefer **dry-run / help / read-only** — nothing billed.
