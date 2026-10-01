// Package store fournit la persistance bbolt et les primitives autonomes de
// chiffrement et d’écriture de fichiers. Les chemins et les clés sont fournis
// par l’appelant ; ce package ne dépend pas du package loom.
//
// Les verrous et le cache de lecture historiques vivent uniquement ici. La base
// logique reste unique par process ; son handle est ouvert et fermé à chaque
// opération. Aucun nouvel état global n’est introduit par cette extraction.
// Le coffre, le verrouillage, les migrations chiffrées et les snapshots restent
// dans loom tant qu’ils dépendent de son état mémoire et de ses chemins.
package store
