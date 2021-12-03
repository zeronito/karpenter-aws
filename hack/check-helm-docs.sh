#!/bin/bash

set -eu -o pipefail

CHART_PATH="$(pwd)/charts/karpenter"

main() {
    generateREADMEShasum
    runHelmDocsAndCheck
}

generateREADMEShasum() {
    shasum ${CHART_PATH}/README.md > ${CHART_PATH}/README.md.sum
}

runHelmDocsAndCheck() {
    helm-docs > /dev/null

    if [[ $(shasum "${CHART_PATH}/README.md") == $(cat "${CHART_PATH}/README.md.sum") ]]; then
        echo "Documentation up to date ✔"
        exit 0
    else
        echo -e "Checksums did not match - Documentation outdated! ❌\n  Before: $(cat ${CHART_PATH}/README.md.sum)\n  After: $(shasum ${CHART_PATH}/README.md)\n  ↳ $ Execute helm-docs and push again"
        exit 1
    fi
}

main "$@"