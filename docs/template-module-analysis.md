# Template Module Analysis

## Goal
Implement an Ansible-like `template` module for Dibra. Rendering happens on the controller, then the rendered content is transferred to the agent for idempotent writes and metadata updates.

## References
- Ansible template module docs: https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/template_module.html
- Ansible template module implementation: https://github.com/ansible/ansible/blob/devel/lib/ansible/modules/template.py
- Ansible integration tests: https://github.com/ansible/ansible/tree/devel/test/integration/targets/template
- Ansible unit tests: https://github.com/ansible/ansible/blob/devel/test/units/template/test_template.py
- Blogposts: spacelift.io, geeksforgeeks, cyberpanel, ansiblepilot

## Template Engine Choice
We selected `github.com/aisbergg/gonja`.
- Jinja2-like syntax, includes `if`, `for`, `include`, `extends`.
- Custom delimiter options: `OptVariableStartString`, `OptVariableEndString`, `OptBlockStartString`, `OptBlockEndString`, `OptCommentStartString`, `OptCommentEndString`.
- Block controls: `OptTrimBlocks`, `OptLstripBlocks`.
- Newline sequence control: `OptNewlineSequence`.
- Undefined handling supports `NewChainedStrictUndefinedValue` to error when references are missing.

## Controller vs Agent
Controller:
- Read template source from controller filesystem.
- Build a template context with vars plus template metadata.
- Render with gonja.
- Send rendered content + file metadata to agent.

Agent:
- Receive rendered content and write to dest.
- Respect `force`, `backup`, `follow`, `validate`, `mode`, `owner`, `group`.
- Use idempotent content hash checks and attribute updates.

## Required Parameters
- `src`: controller-side path to template file.
- `dest`: target path on remote.

## Supported Parameters (Phase 1)
- `src`, `dest`
- `mode`, `owner`, `group`
- `backup`
- `force` (default true)
- `follow`
- `validate` (command with `%s`)
- `newline_sequence` (`\n`, `\r`, `\r\n`)
- `trim_blocks`, `lstrip_blocks`
- `variable_start_string`, `variable_end_string`, `block_start_string`, `block_end_string`, `comment_start_string`, `comment_end_string`

## Template Metadata Variables
Populate these into the template context:
- `ansible_managed`
- `template_host`
- `template_uid`
- `template_path`
- `template_fullpath`
- `template_destpath`
- `template_dest`
- `template_run_date`
- `template` (map containing the template metadata fields)

## Idempotency
- Use SHA1 checksum of rendered content to decide whether to update.
- If `force=false` and dest exists, do not overwrite.
- Always reapply ownership/mode changes when content is identical.

## Rendering Semantics
- Use a filesystem loader for includes/extends rooted at the template's directory.
- Use strict undefineds so missing variables error.
- Add `newline_sequence` normalization after rendering (per Ansible semantics).

## Return Fields
- `changed`, `failed`, `msg`
- `dest`, `src`, `checksum`, `backup_file`, `size`, `mode`, `owner`, `group`

## Integration Test Plan
Inspired by Ansible's template integration tests, focus on:
- Basic render with vars.
- Dest as directory (dest ends with `/`).
- Custom delimiters.
- Trim blocks and lstrip blocks behavior.
- Idempotency (second run no change).
- `force=false` prevents overwrite.
- `validate` command rejects invalid content.
- `newline_sequence` conversion to CRLF.
- Nested include rendering (`{% include %}`).
- Register fields expose checksums, dest, etc.

## Unit Test Plan
Use Go unit tests around renderer:
- Resolve paths.
- Custom delimiter behavior.
- Error on undefined variable.
- Trim/lstrip output semantics.
- Include/extends resolution.

## Gaps vs Ansible
Not yet implemented:
- `mode: preserve` behavior.
- SELinux attributes, `unsafe_writes`.
- `output_encoding`.
- `attributes` (chattr).
- Full Jinja2 filter/test parity (Gonja has a subset).
These can be future phases if needed.
