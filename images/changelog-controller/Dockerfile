FROM registry.ci.openshift.org/openshift/release:rhel-9-release-golang-1.26-openshift-5.0 AS builder
WORKDIR /go/src/github.com/openshift-eng/changelog-controller
COPY . .
RUN CGO_ENABLED=0 go build -o changelog-controller ./cmd/changelog-controller

FROM registry.apps.engineering.openshift.org/openshift/tools:latest
COPY --from=builder /go/src/github.com/openshift-eng/changelog-controller/changelog-controller /usr/bin/changelog-controller
ENTRYPOINT ["/usr/bin/changelog-controller"]
