# Site Sentinel, for coding agents

Site Sentinel watches web sites from this machine: whether each answers, what
certificate it serves, where it resolves, and when its domain renews. It can
also hold a watch open and wake you when something changes.

## When to use it, and when not

Use it to read what the sentinel has already observed, and to arm a watch that
outlives your session.

Do not use it as a general HTTP client. It reports the sentinel's own periodic
observations, not a fresh fetch: if you need the current bytes of a page, fetch
the page. Ask the sentinel when the question is "is this still up", "how long
has it been down", "when does this certificate expire" or "tell me when this
changes".

It performs no authenticated checks and holds no credentials for any site it
watches. It cannot log in anywhere, and it will not.

## Where the binary is

It is not on `PATH`. It lives in the plugin folder:

    ~/.config/omarchy/plugins/karamble.sitesentinel/bin/sentinel

Fall back to `command -v sentinel` if that path does not exist. The short form
`sentinel` is used everywhere below.

## Before acting

Read the existing list first with `sentinel sites`, and do not add a site that
is already there: adding a duplicate URL is refused, and adding the same site
under a second name splits its history.

Never remove or edit a watch another agent armed. `sentinel alerts` shows who
armed each one in `armedBy`. If a watch is in the way, say whose it is rather
than taking it down.

Relay any warning printed on stderr rather than swallowing it.

## Reading

| Command | Answers |
| --- | --- |
| `sentinel status` | one line per site, worst first |
| `sentinel sites` | every site in full, as JSON |
| `sentinel certs` | certificates, soonest to expire first |
| `sentinel domains` | registrations, soonest to renew first |
| `sentinel health` | whether the daemon is awake, when each tier last ran |

Three things in the output change how the rest should be read:

- **`localFault: true`** means this machine has no usable connectivity. Nothing
  in that answer is evidence about any site. Do not report sites as down.
- **`monitoring: false`** means checking is switched off and everything shown is
  the last known state.
- **`domainExpiryUnknown: true`** means the registry publishes no expiry date.
  That is not an imminent expiry, and must never be reported as one.

A certificate can be invalid while far from expiry. Read `certValid`, not only
`certDaysLeft`.

## Changing what is watched

    sentinel add https://example.com -name "Example" -expect "Welcome"
    sentinel pause example.com
    sentinel resume example.com
    sentinel remove example.com

`-expect` matters. Without it a page returning 200 with an error on it counts as
healthy, which is the most common way monitoring lies.

## Arming a watch

    sentinel arm -path <path> -op <operator> -expires <span> [flags]

Required on every watch: `-path`, `-op` and `-expires`. Run
`sentinel catalogue` for the paths and the operators each one accepts.

| Flag | Applies to | Value |
| --- | --- | --- |
| `-above`, `-below` | crosses, count | a number; give one, never both |
| `-value` | becomes | the value to wait for |
| `-older-than` | ages | a span such as `7d` |
| `-field` | ages | which timestamp to measure |
| `-where` | any list | `field=value` or `field~=text`; repeatable |
| `-to` | any | `you` for a desktop notification, or an agent or pane id |
| `-reason` | any | why it matters |
| `-standing` | any | ring every time, rather than once |
| `-armed-by` | any | your own id, so others can see whose watch it is |

Spans are `90m`, `12h`, `7d`, a date like `2026-12-01`, or an RFC 3339 time.

## What arrives

The alarm reaches you as one line:

    sentinel alarm 7f3a91: sites.downList appeared: shop.example.com

It carries the condition, the time and the reason, and **no values**. That is
deliberate: it stays true whenever it is delivered. Read the sentinel yourself
for the numbers, with `sentinel status` or the `sentinel_sites` MCP tool.

Worked examples are in `sentinel skill -recipes`.
