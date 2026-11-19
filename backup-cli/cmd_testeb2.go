package main

import (
	"fmt"

	"mgsis-backup/b2"
)

// runTesteB2All tests every database's key and bucket, continuing past
// failures so one run shows the state of all of them.
func runTesteB2All(cfg Config) int {
	targets := cfg.Targets()
	failed := 0
	for i, t := range targets {
		fmt.Printf("[%d/%d] cliente %s (banco %s)\n", i+1, len(targets), t.ClientID, t.PGDatabase)
		if runTesteB2(t) != 0 {
			failed++
		}
		fmt.Println()
	}
	if failed > 0 {
		fmt.Printf("FALHA em %d de %d banco(s).\n", failed, len(targets))
		return 1
	}
	fmt.Printf("Todos os %d banco(s) com acesso ao B2 confirmado.\n", len(targets))
	return 0
}

// runTesteB2 tests one database's key and bucket (cfg as returned by
// Config.Targets).
func runTesteB2(cfg Config) int {
	fmt.Printf("Testando autenticação e acesso ao bucket %s...\n", cfg.B2Bucket)

	client := b2.New(cfg.B2KeyID, cfg.B2AppKey)
	if err := client.Authorize(); err != nil {
		fmt.Println("FALHA ao autenticar:", err)
		return 1
	}

	bucket, err := client.FindBucketByName(cfg.B2Bucket)
	if err != nil {
		fmt.Println("FALHA ao localizar o bucket:", err)
		return 1
	}
	if bucket == nil {
		fmt.Printf("FALHA: bucket %q não encontrado, ou esta chave não tem acesso a ele.\n", cfg.B2Bucket)
		return 1
	}

	if err := client.ListFileNames(bucket.BucketID, 1); err != nil {
		fmt.Println("FALHA ao listar arquivos do bucket:", err)
		return 1
	}

	fmt.Println("SUCESSO: autenticação e acesso ao bucket confirmados.")
	return 0
}
