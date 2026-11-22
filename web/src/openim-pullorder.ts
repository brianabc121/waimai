// @openim/client-sdk imports this enum from a file its protocol package ships
// only as a type declaration. vite.config.ts points that import here.
export const PullOrder = { PullOrderAsc: 0, PullOrderDesc: 1, UNRECOGNIZED: -1 } as const
