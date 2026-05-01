# Contribuindo

Obrigado por querer contribuir.

## Ambiente

- Go instalado
- Docker rodando (para testes de integração)

## Como validar antes de abrir PR

```bash
go test ./...
```

Para pular integrações:

```bash
go test -short ./...
```

## Padrões

- Prefira mudanças pequenas e revisáveis.
- Mantenha as mensagens e erros em português (coerente com o código atual).
- Testes de integração devem ser condicionados a `testing.Short()`.

