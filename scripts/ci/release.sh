#! /bin/sh
rm .git/hooks/*
npm i conventional-changelog-conventionalcommits@7.0.2 @semantic-release/release-notes-generator@12.1.0
sg release -i npm || true
