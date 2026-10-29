FROM redpandadata/connect:4.92.0

COPY ./configs /configs

CMD [ "streams", "-o", "/configs/observability.yaml", "/configs/streams/*.yaml" ]