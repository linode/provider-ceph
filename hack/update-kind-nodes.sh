#!/bin/bash

: "${LATEST_KIND_NODE? = required}"

sed -i "/^TEST_KIND_NODES/ s/$/,${LATEST_KIND_NODE}/" Makefile
sed -i 's|\(TEST_KIND_NODES ?=\)[^,]*|\1|' Makefile
sed -i '/^TEST_KIND_NODES/ s/?=,/?= /g' Makefile

