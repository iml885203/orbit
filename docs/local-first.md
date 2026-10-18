# Use Orbit with your project

[English](./local-first.md) · [繁體中文](./local-first.zh-TW.md)

From the project you want to run, give your coding agent one request:

> Read https://orbit.dotw.me and help me get this project running with Orbit.
> You may install the official Orbit CLI and plugin. Ask before installing
> other software or making destructive changes.

The agent inspects the existing setup, starts the intended environment,
verifies representative application behavior, and reports anything that still
needs you. You do not need to choose an install method or learn Orbit first.

## What your agent reads

The agent follows the published Orbit skill at
<https://orbit.dotw.me/agent/SKILL.md>, also linked as **For agents** in the
site navigation. In short: it installs the official Orbit CLI and plugin and
asks before installing anything else, describes the project's existing setup in
a project-local `orbit.yaml` instead of running the public demo, starts the
environment, and proves a representative application flow responds before
reporting back. The
skill is the authoritative description of that loop; this page only points to
it.

For manual use, see [Installation](development.md),
[Configuration](configuration.md), and the
[runnable example](https://github.com/iml885203/orbit/tree/main/docs/examples/local-first).
For a shared environment repository, see [Team adoption](team-adoption.md).
