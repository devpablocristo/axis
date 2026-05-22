# Axis Console

UI operativa/admin para Axis.

El browser no debe llamar directo a `../companion` ni `../nexus`; todo acceso
operativo debe ir por `../bff`, que resolverá auth, tenant, scopes e identidad
delegada.
