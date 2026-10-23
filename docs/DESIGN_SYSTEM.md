# Express console design system

The console in `web/` is the publisher surface for Solar Network releases: the
marketplace, a publisher's products, and the release console that uploads
artifacts and publishes versions. DistributionCenter serves it itself from
`[web] distPath`, so this document covers both the deployment and the Vite
development server.

It is the same console system as Stargate's (`Stargate/docs/design-system.md`),
re-derived for release work. Read that document for the reasoning behind the
palette and the typography; this one records what Express adds to it.

## 1. What is shared

- **One signal color.** Periwinkle (`primary`) marks the action or the current
  location. Semantic colors appear only when state needs them: `success` for a
  live release, `warning` for a draft, `error` for destruction, `faint` for
  anything retired. Brass (`accent`) is unused here — nothing in the console is
  a privilege marker.
- **Machine data and human explanation are typeset differently.** Versions,
  slugs, targets, hashes, and counts are mono. Guidance and empty states are
  `.voice` (sans).
- **Blocks, not rules.** Panels are `bg-base-200/30` with a hairline border.
  Hairlines survive inside a ledger and inside a panel header.
- **The shell is a top bar everywhere, with a sidebar where a tree exists.**
  A release console is a dense ledger surface, so the page is one wide column
  rather than a centered card. The marketplace, a public product page, and the
  workspace read as a single wide page; the sidebar belongs to the console,
  where the account's publishers and products are navigation worth showing.

Tokens, themes, and base chrome live in `web/src/assets/main.css`. The theme
names are `express` (dark, default) and `express-light`; the palette values are
identical to Stargate's, so a reader moving between the two consoles sees no
color shift.

## 2. Release vocabulary

The console prints the API's own words, because a publisher has to be able to
match what the console shows to what the API accepted:

| Concept | Word in the console | Wire value |
| --- | --- | --- |
| private release | Draft | `draft` |
| served release | Published | `published` |
| retired release | Yanked | `yanked` |
| release stream | Channel | `stable`, `beta`, `nightly`, `rolling`, custom |
| one file | Artifact | `platform` + `architecture` |
| CI credential | Upload key | — |

A release that a client can download is **published**; there is no softer word
for it. A channel is not a "track" and an artifact is not a "build".

## 3. Surfaces

| Route | Content |
| --- | --- |
| `/marketplace` | public catalog, one row per product |
| `/marketplace/:productId` | public product page: channels, notes, downloads |
| `/workspace` | the account's products, grouped by publisher |
| `/workspace/:productId` | the console: overview, releases, channels, keys, metrics, sync |
| `/workspace/:productId/releases/:releaseId` | one release: artifacts, notes, metadata |

The console is **one route with tabs**, not one route per tab: the tabs switch
state, so a reload lands back on the same product rather than on a tab whose
product has to be re-resolved. The release detail is its own route because it
is the page people link to when asking about a version.

## 4. Layout

```
┌─ top bar ────────────────────────────────────────────────┐
│ ☰  EXPRESS  Solar Network   [Marketplace] [Workspace]  ⌾ │
├─ sidebar (256 px) ─┬─ main ──────────────────────────────┤
│ ACME INDUSTRIES  1 │  page header (title, description)   │
│  ▸ Desktop Client  │                                     │
│                    │  panels / ledgers                   │
│                    │                                     │
│                    │  status rail (OK/ERROR + message)   │
└────────────────────┴─────────────────────────────────────┘
```

The sidebar is mounted by route metadata (`meta.sidebar`), which the console
and the release detail set. It is sticky from `lg` up and a drawer below it,
opened by the hamburger in the top bar. It lists one group per publisher the
account belongs to, with each group's products beneath it; the open product is
filled with `base-300`. A deployment's account with no publisher sees the
group list empty and the workspace page explains why.

Scoping it to the console is deliberate: the marketplace is a public catalogue
and the workspace is a list of publishers, and neither has a tree to put beside
it. A rail that is always present would also cost 256 px of the width the
tables need.

The main column is capped at 1440 px so a 4K display does not stretch a
six-column ledger past readability. `meta.narrow` narrows a page to 560 px for
the rare form-shaped route.

The status rail is sticky to the bottom of the main column: a result stays
visible where the work happened, the way a terminal prints it, without
floating over the content.

### View transitions

Route changes cross-fade instead of swapping. Two entry points opt in:

- A `RouterLink` carries `view-transition`; vue-router then wraps the
  navigation in `document.startViewTransition`.
- `navigate()` (in `lib/viewTransition.ts`) does the same for a programmatic
  `push`, and `withViewTransition()` wraps a local state change — the console
  tab strip uses it so the panel cross-fades while the page frame stays put.

The effect is a progressive enhancement. `withViewTransition` runs the update
directly when the browser has no `startViewTransition`, and when the reader
set `prefers-reduced-motion`. A transition superseded by a newer navigation
still applied its update, so `finished` rejecting is not an error; the promise
the caller awaits is the update itself.

Only the work column animates: the sizing and chrome live in
`assets/transitions.css`, and the console panel is named only for the duration
of a tab switch, so a normal navigation never has to share that name.

## 5. Components

`PageHeader`, `ConsolePanel`, `LoadingSkeleton`, `IconButton`, `CopyButton`,
`ConfirmAction`, `TabBox`, `AppMark`, and `SessionGate` are the shared
surfaces. Every grouped region is a `ConsolePanel`; no page re-implements its
shell.

`AppMark` renders a product's picture, then its monogram, then a neutral glyph,
so a deleted file never leaves a hole in a row.

`SessionGate` owns the signed-out, offline, and no-publisher states. A gated
page names what it needs and offers the one action that resolves it instead of
rendering an empty shell.

### Sign-in

The shell has exactly one sign-in affordance, in the top bar, and it leaves the
console: `signInUrl()` points at the identity host with the current page as
`redirectUri`. There is no local password form and no token is ever printed.

The return trip lands on a page carrying `?code=…`. The console exchanges it
once, strips it from the address bar before the exchange resolves, and then
renders the same signed-in shell as a returning visitor. A failed exchange is
reported in the status rail ("Sign-in did not complete") and the shell stays
signed out — never a spinner, never a half-signed-in page.

A 401 does not paint a signed-out shell on its own: the console renews the
access token once and repeats the request, so a short token lifetime is
invisible. Only a refused refresh shows the signed-out state.

Signed-out copy names the action, not the mechanism: "Sign in to publish", not
"no AuthToken cookie". Signing out is the account menu's last item and clears
this console's session only.

### Ledger

`table.ledger` is the only table style. Every table carries `ledger-stack`:

```html
<table class="ledger ledger-stack">
```

Below 640 px the table becomes a list of labelled rows. Each cell prints its
column name through `:data-label="t('…')"`, taken from the header it replaces,
so the stack needs no duplicated text and no second catalog. A cell whose value
is itself a block (a file name above its MIME type) wraps that value in
`.cell-stack` so the label sits beside the group rather than inside it.

The rules that make this work:

- The first cell is the row's name and reads full width, left aligned.
- A cell with no label is an action list; it right-aligns with no label.
- `.table-bleed` is dropped at that width: stacked rows sit on the panel's own
  16 px padding, because meeting the panel edge only reads correctly when the
  rows are still columns.

### TabBox

The strip is a single line from `sm` up and a three-column grid below it. Six
console tabs do not fit a 390 px card in one row, and scrolling them would hide
the tab that names the current section. The hairlines are the container's own
background showing through 1 px gaps, so any tab count divides evenly.

## 6. Forms and dialogs

Labels sit above the field (`.field-label`, uppercase 11 px). Inputs, selects,
and textareas are full width, `base-200` fill, hairline border, periwinkle focus
outline. Placeholders are examples, never a repeat of the label.

`ConfirmAction` is the only modal for a destructive or irreversible step:
publish, yank, delete. It states what changes and cannot be undone. Creation
and editing forms are `<dialog class="modal modal-open">` panels with a footer
of Cancel plus one primary action.

## 7. Copy

English and Simplified Chinese, in `web/src/locales/{en,zh-CN}/console.ts`. Both
catalogs have identical key shapes; keys reached only through a template
literal (`releases.status.*`) are the only exception to a literal `t()` call.

- Sentence case in English; full-width punctuation in Chinese sentences.
- No trailing punctuation on labels, buttons, or tabs.
- Errors say what happened and what to do next.
- Empty states invite the next action.
- Do not expose implementation detail: say "upload key", not "bearer token";
  say "artifact", not "S3 object".

Publisher-supplied text (product names, release titles, notes) is resolved with
`localized()` against the reader's locale, with the publisher's default string as
the fallback. A row that prints only the default would show an English title in
a Chinese console.

## 8. Accessibility

- Visible `:focus-visible` outline in primary, 2 px offset.
- Icon-only controls carry both `aria-label` and `title`.
- Tabs use `aria-pressed`; the strip has an `aria-label`.
- Tables use `scope="col"`; an action column is `sr-only`.
- Body text is 13 px; the 11 px floor is limited to labels and metadata.
- Layout works from 360 px wide. A ledger stacks; it never scrolls sideways.

## 9. Adding a screen

1. Register the route with a `titleKey`; set `narrow` only for a form.
2. Build the page from `PageHeader` and `ConsolePanel`.
3. Use `ledger ledger-stack` for tabular data, with `:data-label` on every cell
   that has a header.
4. Use `IconButton` for row actions; native `title` for tooltips.
5. Gate a control on `session.can('…')` with the node the server checks, so it
   is hidden rather than failing with 403.
6. Add copy to both locale modules with identical key shapes.
7. Run `bun run type-check` and `bun run build-only` in `web/`.
