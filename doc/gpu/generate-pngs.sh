#!/bin/bash

if ! type plantuml &> /dev/null; then
    echo "ERR: No plantuml found in PATH, plantuml is needed to produce PNG files"
    exit 1
fi

# source files are in script dir
dir=${0%/*}

for puml in "$dir"/*puml; do
    png="${puml%.puml}.png"
    # update if PNG missing or older that source file
    if test "$puml" -nt "$png"; then
        echo "$puml"
        plantuml "$puml" "$png"
    fi
done
