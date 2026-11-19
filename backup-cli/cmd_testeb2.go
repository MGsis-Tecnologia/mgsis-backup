package main

import (
	"fmt"

	"mgsis-backup/b2"
)

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
