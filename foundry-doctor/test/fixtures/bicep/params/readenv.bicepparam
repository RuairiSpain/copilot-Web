using 'm.bicep'

// BCP427 offline: the doctor never forwards environment variables to the compiler.
param token = readEnvironmentVariable('FD_FIXTURE_VALUE')
