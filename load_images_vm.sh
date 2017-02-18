#!/bin/bash

set -e

MACHINE_ENV=$(docker-machine env $1)
for image in alexmavr/swarm-nbt:latest alexmavr/swarm-nbt-prometheus:latest grafana/grafana:latest; do
    # Check to see if the image ID's match
    OUR_ID=$(docker inspect ${image}|jq -r ".[].Id")
    REMOTE_ID=$( (eval "${MACHINE_ENV}" ; docker inspect ${image} 2>/dev/null ) |jq -r ".[].Id")
    if [ "${OUR_ID}" != "${REMOTE_ID}" ] ; then
        TRANSFER_IMAGES="${TRANSFER_IMAGES} ${image}"
    else
        echo "Image ${image} already in sync"
    fi
done
if [ -n "${TRANSFER_IMAGES}" ] ; then
    echo "Migrating ${TRANSFER_IMAGES} to $1..."
    docker save ${TRANSFER_IMAGES} | (eval "${MACHINE_ENV}" ; docker load)
fi
