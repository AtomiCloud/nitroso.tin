#! /bin/sh
rm .git/hooks/*
npm i conventional-changelog-conventionalcommits@7.0.2
sg release -i npm || true
