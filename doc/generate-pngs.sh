#!/bin/bash

if ! type plantuml &> /dev/null; then
    echo "ERR: No plantuml found in PATH, plantuml is needed to produce PNG files"
    exit 1
fi

for puml in ./*puml; do
    png="${puml%.puml}.png"
    [[ -f "$png" ]] || {
        echo $puml
        plantuml "$puml" "$png"
    }
done
