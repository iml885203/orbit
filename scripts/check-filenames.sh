#!/bin/sh
# Enforce file-name conventions from docs/CODE_CONVENTIONS.md §4 and
# .claude/rules/domain-organization.md: file names must describe a domain,
# not a layer. utils/helpers/common/services/misc are magnets for
# unrelated code — keep them out.
#
# Scope: source files only. Test files are exempt because helpers used
# only by tests don't accrue the same drift risk.
#
# Exit status: 1 if any forbidden name is present, 0 otherwise.

set -e

# Forbidden basenames (without extension). Extend cautiously — every name
# added here is a promise that the alternative is always strictly better.
GO_FORBIDDEN='^(utils|helpers|common|services|misc)\.go$'
TS_FORBIDDEN='^(utils|helpers|common|services|misc)\.(ts|js)$'

violations=$(
  {
    find . -type f -name '*.go' \
      -not -path './vendor/*' \
      -not -path './.claude/*' \
      -not -name '*_test.go' \
      | sed 's|.*/||' \
      | awk -v r="$GO_FORBIDDEN" 'BEGIN{IGNORECASE=0} $0 ~ r'
    find ./ui/src -type f \( -name '*.ts' -o -name '*.js' \) 2>/dev/null \
      -not -name '*.test.ts' \
      -not -name '*.gen.ts' \
      | sed 's|.*/||' \
      | awk -v r="$TS_FORBIDDEN" 'BEGIN{IGNORECASE=0} $0 ~ r'
  } 2>/dev/null || true
)

if [ -z "$violations" ]; then
  exit 0
fi

echo "❌ Forbidden file names (see .claude/rules/domain-organization.md):"
# Re-list with full paths so the violation is actionable.
find . -type f \( -name '*.go' -o -name '*.ts' -o -name '*.js' \) \
  -not -path './vendor/*' \
  -not -path './ui/node_modules/*' \
  -not -path './.claude/*' \
  -not -name '*_test.go' \
  -not -name '*.test.ts' \
  | grep -E '/(utils|helpers|common|services|misc)\.(go|ts|js)$' \
  | sed 's|^|  |'
echo
echo "Rename to describe the domain it owns. Example: utils.go → fsutil.go,"
echo "helpers.ts → graphActions.ts."
exit 1
