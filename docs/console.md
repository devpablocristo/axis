# Axis Console

UI operativa/admin para Axis.

El browser no debe llamar directo a `../companion` ni `../nexus`; todo acceso
operativo debe ir por `../bff`, que resolverá auth, tenant, scopes e identidad
delegada.

## Virtual Employees

Console muestra **Virtual Employees** como recurso operativo principal para
trabajadores digitales persistentes. La pantalla reutiliza el CRUD historico de
Agents, pero consume `/api/virtual-employees`.

`/api/agents` queda como compatibilidad tecnica del BFF; la UI nueva no debe
presentar Agents como recurso principal salvo en contextos internos o legacy.
