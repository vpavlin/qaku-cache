FROM registry.access.redhat.com/ubi9/ubi:latest@sha256:66233eebd72bb5baa25190d4f55e1dc3fff3a9b77186c1f91a0abdb274452072 


ADD _build/cachenode /usr/local/bin/cachenode
ADD _libwaku/* /lib64/

CMD [ "cachenode" ]