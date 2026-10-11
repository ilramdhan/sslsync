## What and why

<!-- What does this change, and what problem does it solve? Link the issue: Closes #123 -->

## Type

- [ ] Bug fix
- [ ] New feature / target type
- [ ] Refactor (no behavior change)
- [ ] Documentation
- [ ] Chore (dependencies, CI, tooling)

## Impact on operators

- [ ] No change to `.env` settings or commands
- [ ] New or changed settings: documented in **README.md** and **.env.example**
- [ ] Breaking: existing `.env` files must change (describe the migration below)

## How was this tested?

<!-- Unit tests, plus for runner/remote/k8s changes: which disposable server or
     container, which scenarios (success, failure → rollback, rerun). Never production. -->

## Checklist

- [ ] `make test` passes
- [ ] `make lint` reports 0 issues
- [ ] Errors tell the operator which variable or command fixes them
- [ ] No secrets, certificates, real host names or `.env` contents in the diff, tests or this description
- [ ] Shell sent to servers is POSIX `sh` and quotes values with `remote.Shq`
- [ ] Anything that changes a server goes through runner phases (checked, verified, rolled back)
