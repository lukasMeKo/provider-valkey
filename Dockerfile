FROM gcr.io/distroless/static:nonroot

ARG TARGETOS
ARG TARGETARCH

COPY bin/${TARGETOS}_${TARGETARCH}/provider /usr/local/bin/provider

USER 65532
ENTRYPOINT ["provider"]
