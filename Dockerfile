FROM registry.svc.ci.openshift.org/openshift/release:golang-1.10 AS builder
WORKDIR /go/src/sigs.k8s.io/cluster-api-provider-aws
COPY . .
# VERSION env gets set in the openshift/release image and refers to the golang version, which interfers with our own
RUN unset VERSION \
 && NO_DOCKER=1 make build

# registry.svc.ci.openshift.org/openshift/origin-v4.0:base contains private yum repos so installation
# of packages fail. Switching to docker.io/centos:7 where all repos are public.
FROM docker.io/centos:7
RUN INSTALL_PKGS=" \
      openssh \
      " && \
    yum install -y $INSTALL_PKGS && \
    rpm -V $INSTALL_PKGS && \
    yum clean all
COPY --from=builder /go/src/sigs.k8s.io/cluster-api-provider-aws/bin/manager /
COPY --from=builder /go/src/sigs.k8s.io/cluster-api-provider-aws/bin/machine-controller-manager /
