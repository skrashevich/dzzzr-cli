// Package agentfiles lets an agent read files the team keeps on this machine:
// notes from a briefing, a scenario draft, a script that decodes something.
//
// Everything is confined to one root directory, given to [Tools] or taken from
// DZZZR_FILES_ROOT. save_local_json can also create a new JSON artifact when
// explicitly requested; it never overwrites a file. This local save is available
// independently of the engine mutation policy. Paths outside the root, including
// escaping symlinks, are refused. Saved JSON must be a UTF-8 object or array,
// no larger than MaxJSONBytes and its mode is 0600. save_local_json accepts
// exactly one of structured data (preferred) or legacy content (whose bytes are
// preserved). assemble_local_json joins saved level objects only when their keys
// exactly cover a previously saved manifest inventory, in manifest order. Its
// maximum is 128 parts and MaxJSONBytes for all input files together and output.
// Neither tool overwrites files or modifies the engine.
package agentfiles
