#!/bin/bash
# Generate a lazy change log.

NEWTAG=$1 # The new release tag that is about to be applied.

cat <<EOF
# Change log for project Korrel8r

This is the project's commit log. It is placeholder until a more user-readable change log is available.

This project uses semantic versioning (https://semver.org)

## Version $NEWTAG

EOF

git log --format="%d- %s" --decorate-refs=refs/tags/* | sed 's/^ *(tag: \(.*\))\(.*\)$/\n## Version \1\n\n\2/'
