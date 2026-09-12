# menus.d — your menu overrides

Anything you put in here is read **before** the shipped menu set in `menus/`,
file by file. Nothing in this directory is tracked by git, so `git pull` never
touches it and you never have to re-copy your customisations after an upgrade.

Mirror the layout of the set you are overriding. To replace the main menu art
of the `v3` set, for example:

```
menus.d/v3/ansi/MAIN.ANS      ← used if present
menus/v3/ansi/MAIN.ANS        ← otherwise
```

Only the files you add here are affected. Overriding one `.ANS` does not mean
copying the rest of `ansi/`; every other file still comes from `menus/`.

- `menuedit` saves into this directory by default (files it wrote are marked
  with `*` in its menu list). Run it with `--no-overlay` to edit `menus/`
  directly.
- `theme.json` can be overridden here too, and hot-reloads like the shipped one.
- A lightbar menu's `.ANS`, `.BAR` and `.CFG` are drawn against each other.
  If you override one, check the others still line up; the BBS logs a warning
  when a lightbar menu's files come from different layers.
- A file cannot be *removed* from the shipped set by way of this directory.

See the sysop guide: `docs/sysop/menus/menu-system.md#customising-menus-without-losing-your-changes`.
