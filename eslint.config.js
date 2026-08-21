import nextCoreWebVitals from "eslint-config-next/core-web-vitals";
import nextTypescript from "eslint-config-next/typescript";

export default [
    { ignores: [".next/**", "dist/**", "node_modules/**"] },
    ...nextCoreWebVitals,
    ...nextTypescript,
];
