package dryrun

// Compatibility recognition and draft rendering for the historical Calendar
// v1 contract. New domain executors provide UUID, operation, schemas and
// arguments through axis.connector.v1 instead of adding selectors here.

import virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"

func legacyIntentDefinitions() []intentDefinition {
	resourceKeywords := []string{
		"calendar", "calendario", "event", "events", "evento", "eventos",
		"reunion", "reuniones", "meeting", "meetings",
	}
	return []intentDefinition{
		{
			Domain: "calendar", Resource: "events", Action: "create",
			CapabilityKey: "calendar.events.create", RequiredAutonomy: virployeedomain.AutonomyA2,
			ResourceKeywords: resourceKeywords,
			ActionKeywords:   []string{"crear", "crea", "create", "agendar", "agenda", "agende", "programar", "programa", "schedule", "book"},
		},
		{
			Domain: "calendar", Resource: "events", Action: "read",
			CapabilityKey: "calendar.events.read", RequiredAutonomy: virployeedomain.AutonomyA1,
			ResourceKeywords: resourceKeywords,
			ActionKeywords:   []string{"leer", "lee", "ver", "listar", "lista", "mostrar", "mostra", "consultar", "consulta", "read", "list", "show", "find", "buscar", "busca", "que", "tengo", "hay"},
		},
		{
			Domain: "calendar", Resource: "events", Action: "update",
			CapabilityKey: "calendar.events.update", RequiredAutonomy: virployeedomain.AutonomyA2,
			ResourceKeywords: resourceKeywords,
			ActionKeywords:   []string{"editar", "edita", "actualizar", "actualiza", "modificar", "modifica", "cambiar", "cambia", "reprogramar", "reprograma", "update", "change", "reschedule"},
		},
		{
			Domain: "calendar", Resource: "events", Action: "delete",
			CapabilityKey: "calendar.events.delete", RequiredAutonomy: virployeedomain.AutonomyA2,
			ResourceKeywords: resourceKeywords,
			ActionKeywords:   []string{"eliminar", "elimina", "borrar", "borra", "cancelar", "cancela", "delete", "remove", "cancel"},
		},
	}
}

func legacyDraft(input string, intent Intent, decision Decision) (Draft, bool) {
	switch intent.CapabilityKey {
	case "calendar.events.create":
		return buildCalendarEventCreateDraft(input, intent, decision), true
	case "calendar.events.read":
		return buildCalendarEventReadDraft(input, intent, decision), true
	case "calendar.events.update":
		return buildCalendarEventUpdateDraft(input, intent, decision), true
	case "calendar.events.delete":
		return buildCalendarEventDeleteDraft(input, intent, decision), true
	default:
		return Draft{}, false
	}
}
