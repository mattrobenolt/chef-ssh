#!/bin/bash
set -ex

rm -rf bin/
docker build --rm -t chef-ssh .
docker run --rm -v $PWD:/go/src/app chef-ssh
