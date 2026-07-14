# SuperMap S3M renderer attribution

The JavaScript sources in this directory are derived from the Apache-2.0
licensed SuperMap S3M implementations:

- https://github.com/SuperMap/s3m-spec/tree/master/S3M_SDK/S3M_JS
- https://github.com/SuperMap/iClient3D-for-WebGL/tree/master/Cesium_S3MLayer_Plugins/S3MTilesLayer

`S3ModelOldParser.js` is retained because SuperMap iObjects Java currently
emits the legacy XML SCP + `.s3m` representation. The remaining parser and
renderer sources implement the shared S3M tile pipeline. Cesium is loaded as
an independent Apache-2.0 dependency only when the S3M preview component is
mounted.

The packaged Draco and Crunch WebAssembly assets are copied at build time from
the MIT-licensed `@dfsj/s3m` npm package; its JavaScript renderer is not used.
