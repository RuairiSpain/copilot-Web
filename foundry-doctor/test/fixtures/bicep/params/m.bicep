param token string
param region string = 'westeurope'

#disable-next-line no-unused-params
param unused string = 'a'

output summary string = '${token}-${region}'
