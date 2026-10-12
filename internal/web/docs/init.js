'use strict';
window.addEventListener('DOMContentLoaded', () => {
  SwaggerUIBundle({
    url: '/openapi.json',
    dom_id: '#swagger-ui',
    deepLinking: true,
    validatorUrl: null,
    persistAuthorization: false,
    queryConfigEnabled: false,
    supportedSubmitMethods: ['get', 'post'],
    presets: [SwaggerUIBundle.presets.apis],
  });
});
